package checker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/bytedance/sonic"
	"github.com/google/generative-ai-go/genai"
	"github.com/rotector/rotector/internal/common/client/fetcher"
	"github.com/rotector/rotector/internal/common/setup"
	"github.com/rotector/rotector/internal/common/storage/database/types"
	"github.com/rotector/rotector/internal/common/translator"
	"github.com/rotector/rotector/internal/common/utils"
	"github.com/tdewolff/minify/v2"
	"github.com/tdewolff/minify/v2/json"
	"go.uber.org/zap"
)

const (
	// ReviewSystemPrompt provides detailed instructions to the AI model for analyzing user content.
	ReviewSystemPrompt = `You are a Roblox moderator analyzing user data for inappropriate sexual or suggestive content.

Analysis Instructions:
- Return ANY user that violates ANY of the guidelines below
- Flag ALL violations, no matter how subtle
- Flag users even if only one suspicious phrase is found
- Use exact strings for 'flaggedContent'
- Do not consider the empty description as suspicious
- When in doubt, flag the user - false positives are better than missing violations
- Never flag the same user multiple times - combine all violations into one entry

Flag content containing:
- Explicit sexual terms and innuendos
- Body part references
- Hookup solicitation
- Porn references
- Suggestive emojis
- NSFW content
- ERP terms
- Fetish mentions
- Dating requests

Flag suspicious phrases like:
- "add me if you wanna have fun"
- "surprise me"
- "limitless"
- "studio" combined with age or roleplay references
- "tell me what u want"
- "older guys >>>"
- "chat rp only"
- Age + "rp" combinations
- "dm me for fun"
- ";)" or ":3" with suggestive context
- "looking for older"
- "looking for fun"
- "its so tight"
- "surprise me"
- "no limits"
- "anything goes"
- "open minded"
- "private chat only"
- "private games only"
- "switch" or "dom" references
- "gas rp" or "fart rp"
- "💿" in roleplay context
- "bulls" with suggestive context
- "mommi" or other infantilizing terms

Flag grooming indicators:
- Age questions
- Photo requests
- Off-platform chat attempts
- Personal info seeking
- Offering special treatment/privileges
- Gift offers
- Secret keeping
- Building trust through compliments
- Sexualized roleplay
- Non-consensual references
- Exploitation/harassment
- Age + roleplay combinations
- Mentions of being "young" or "older"
- References to age gaps

Flag predatory behavior:
- Love bombing
- Isolation attempts
- Manipulation
- Guilt-tripping tactics
- Camera/mic usage requests
- Private game invites
- Social media contact attempts
- Adult industry references (OnlyFans, modeling scams, compensation offers)
- Mentions of "studio" in suspicious context
- Requests for private interactions
- References to age preferences
- Suggestive emoji combinations

Confidence Calculation (start at 0):
- +0.6: Explicit violations
- +0.4: Clear suggestive content or suspicious combinations
- +0.3: Combinations of age + suspicious terms
- +0.3: Known grooming patterns
- +0.3: Suspicious use of "studio" or roleplay terms
- +0.2: Subtle hints or concerning patterns

Confidence Levels:
- High (0.8-1.0): Explicit content or multiple violations
- Medium (0.4-0.7): Clear patterns or suspicious combinations
- Low (0.0-0.3): Subtle or ambiguous content - should rarely be used`

	// FriendSystemPrompt provides detailed instructions to the AI model for analyzing friend networks.
	FriendSystemPrompt = `You are a content moderation assistant analyzing user friend networks for inappropriate patterns.

Analysis Focus:
- Examine common violation themes
- Assess content severity
- Evaluate network concentration
- Generate clear, short, factual 1-sentence reason
- Highlight most serious violations and patterns
- Never include usernames in the reason

Confidence Calculation (start at 0):
- +0.6: Multiple confirmed friends with serious violations
- +0.4: Multiple flagged friends with clear patterns
- +0.2: Mixed confirmed/flagged friends
- +0.1: Each additional same-type violation
- +0.2: Each different violation type

Confidence Levels:
- High (0.8-1.0): Strong confirmed networks
- Medium (0.4-0.7): Clear patterns with mixed status
- Low (0.0-0.3): Limited connections`

	// FriendUserPrompt is the prompt for analyzing a user's friend network.
	FriendUserPrompt = `User: %s
Friend data: %s`
)

// MaxFriendDataTokens is the maximum number of tokens allowed for friend data.
const MaxFriendDataTokens = 400

// Package-level errors.
var (
	// ErrModelResponse indicates the model returned no usable response.
	ErrModelResponse = errors.New("model response error")
	// ErrJSONProcessing indicates a JSON processing error.
	ErrJSONProcessing = errors.New("JSON processing error")
)

// FlaggedUsers holds a list of users that the AI has identified as inappropriate.
// The JSON schema is used to ensure consistent responses from the AI.
type FlaggedUsers struct {
	Users []FlaggedUser `json:"users"`
}

// FlaggedUser contains the AI's analysis results for a single user.
// The confidence score and flagged content help moderators make decisions.
type FlaggedUser struct {
	Name           string   `json:"name"`
	Reason         string   `json:"reason"`
	FlaggedContent []string `json:"flaggedContent"`
	Confidence     float64  `json:"confidence"`
}

// AIChecker handles AI-based content analysis using Gemini models.
type AIChecker struct {
	userModel   *genai.GenerativeModel // For analyzing user content
	friendModel *genai.GenerativeModel // For analyzing friend networks
	minify      *minify.M
	translator  *translator.Translator
	logger      *zap.Logger
}

// NewAIChecker creates an AIChecker with separate models for user and friend analysis.
func NewAIChecker(app *setup.App, translator *translator.Translator, logger *zap.Logger) *AIChecker {
	// Create user analysis model
	userModel := app.GenAIClient.GenerativeModel(app.Config.Common.GeminiAI.Model)
	userModel.SystemInstruction = genai.NewUserContent(genai.Text(ReviewSystemPrompt))
	userModel.GenerationConfig.ResponseMIMEType = "application/json"
	userModel.GenerationConfig.ResponseSchema = &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"users": {
				Type: genai.TypeArray,
				Items: &genai.Schema{
					Type: genai.TypeObject,
					Properties: map[string]*genai.Schema{
						"name": {
							Type:        genai.TypeString,
							Description: "Exact username of the flagged user",
						},
						"reason": {
							Type:        genai.TypeString,
							Description: "Clear explanation of why the user was flagged",
						},
						"flaggedContent": {
							Type: genai.TypeArray,
							Items: &genai.Schema{
								Type: genai.TypeString,
							},
							Description: "Exact content that was flagged",
						},
						"confidence": {
							Type:        genai.TypeNumber,
							Description: "Confidence level of the AI's assessment",
						},
					},
					Required: []string{"name", "reason", "flaggedContent", "confidence"},
				},
			},
		},
		Required: []string{"users"},
	}
	userTemp := float32(1.0)
	userModel.Temperature = &userTemp

	// Create friend analysis model
	friendModel := app.GenAIClient.GenerativeModel(app.Config.Common.GeminiAI.Model)
	friendModel.SystemInstruction = genai.NewUserContent(genai.Text(FriendSystemPrompt))
	friendModel.GenerationConfig.ResponseMIMEType = "application/json"
	friendModel.GenerationConfig.ResponseSchema = &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"name": {
				Type:        genai.TypeString,
				Description: "Username being analyzed",
			},
			"reason": {
				Type:        genai.TypeString,
				Description: "Analysis of friend network patterns",
			},
			"confidence": {
				Type:        genai.TypeNumber,
				Description: "Confidence level in the analysis",
			},
		},
		Required: []string{"name", "reason", "confidence"},
	}
	friendTemp := float32(0.8)
	friendModel.Temperature = &friendTemp

	// Create a minifier for JSON optimization
	m := minify.New()
	m.AddFunc("application/json", json.Minify)

	return &AIChecker{
		userModel:   userModel,
		friendModel: friendModel,
		minify:      m,
		translator:  translator,
		logger:      logger,
	}
}

// ProcessUsers sends user information to OpenAI for analysis after translating descriptions.
// Returns validated users and IDs of users that failed validation for retry.
// The process involves:
// 1. Translating user descriptions to proper English
// 2. Sending translated content to OpenAI for analysis
// 3. Validating AI responses against translated content
// 4. Creating validated users with original descriptions.
func (a *AIChecker) ProcessUsers(userInfos []*fetcher.Info) (map[uint64]*types.User, []uint64, error) {
	// Create a struct for user summaries for AI analysis
	type UserSummary struct {
		Name        string `json:"name"`
		DisplayName string `json:"displayName,omitempty"`
		Description string `json:"description"`
	}

	// Translate all descriptions concurrently
	translatedInfos, originalInfos := a.prepareUserInfos(userInfos)

	// Convert map to slice for OpenAI request
	userInfosWithoutID := make([]UserSummary, 0, len(translatedInfos))
	for _, userInfo := range translatedInfos {
		summary := UserSummary{
			Name:        userInfo.Name,
			Description: userInfo.Description,
		}
		// Only include display name if it's different from the username
		if userInfo.DisplayName != userInfo.Name {
			summary.DisplayName = userInfo.DisplayName
		}
		userInfosWithoutID = append(userInfosWithoutID, summary)
	}

	// Minify JSON to reduce token usage
	userInfoJSON, err := sonic.Marshal(userInfosWithoutID)
	if err != nil {
		a.logger.Error("Error marshaling user info", zap.Error(err))
		return nil, nil, fmt.Errorf("%w: %w", ErrJSONProcessing, err)
	}

	userInfoJSON, err = a.minify.Bytes("application/json", userInfoJSON)
	if err != nil {
		a.logger.Error("Error minifying user info", zap.Error(err))
		return nil, nil, fmt.Errorf("%w: %w", ErrJSONProcessing, err)
	}

	// Generate content using Gemini model
	resp, err := a.userModel.GenerateContent(context.Background(), genai.Text(string(userInfoJSON)))
	if err != nil {
		a.logger.Error("Error calling Gemini API", zap.Error(err))
		return nil, nil, fmt.Errorf("%w: %w", ErrModelResponse, err)
	}

	// Check for empty response
	if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
		return nil, nil, fmt.Errorf("%w: no response from Gemini", ErrModelResponse)
	}

	// Extract response text from Gemini's response
	responseText := resp.Candidates[0].Content.Parts[0].(genai.Text)

	// Parse Gemini response into FlaggedUsers struct
	var flaggedUsers FlaggedUsers
	err = sonic.Unmarshal([]byte(responseText), &flaggedUsers)
	if err != nil {
		a.logger.Error("Error unmarshaling flagged users", zap.Error(err))
		return nil, nil, fmt.Errorf("%w: %w", ErrJSONProcessing, err)
	}

	a.logger.Info("Received AI response",
		zap.Int("totalUsers", len(userInfos)),
		zap.Int("flaggedUsers", len(flaggedUsers.Users)))

	// Validate AI responses against translated content but use original descriptions for storage
	validatedUsers, failedValidationIDs := a.validateFlaggedUsers(flaggedUsers, translatedInfos, originalInfos)

	return validatedUsers, failedValidationIDs, nil
}

// GenerateFriendReason uses AI to analyze a user's friend list and generate a detailed reason
// for flagging based on the patterns found in their friends' reasons.
func (a *AIChecker) GenerateFriendReason(userInfo *fetcher.Info, confirmedFriends, flaggedFriends map[uint64]*types.User) (string, error) {
	// Create a summary of friend data for AI analysis
	type FriendSummary struct {
		Name   string         `json:"name"`
		Reason string         `json:"reason"`
		Type   types.UserType `json:"type"`
	}

	// Collect friend summaries with token counting
	friendSummaries := make([]FriendSummary, 0, len(confirmedFriends)+len(flaggedFriends))

	// Helper function to add friend if within token limit
	currentTokens := int32(0)
	addFriend := func(friend *types.User, friendType types.UserType) bool {
		summary := FriendSummary{
			Name:   friend.Name,
			Reason: friend.Reason,
			Type:   friendType,
		}

		// Convert to JSON to count tokens accurately
		summaryJSON, err := sonic.Marshal(summary)
		if err != nil {
			a.logger.Warn("Failed to marshal friend summary",
				zap.String("username", friend.Name),
				zap.Error(err))
			return false
		}

		tokenCount, err := a.friendModel.CountTokens(context.Background(), genai.Text(summaryJSON))
		if err != nil {
			a.logger.Warn("Failed to count tokens for friend summary",
				zap.String("username", friend.Name),
				zap.Error(err))
			return false
		}

		currentTokens += tokenCount.TotalTokens
		if currentTokens > MaxFriendDataTokens {
			return false
		}

		friendSummaries = append(friendSummaries, summary)
		return true
	}

	// Add confirmed friends first (they're usually more important)
	for _, friend := range confirmedFriends {
		if !addFriend(friend, types.UserTypeConfirmed) {
			break
		}
	}

	// Add flagged friends if there's room
	for _, friend := range flaggedFriends {
		if !addFriend(friend, types.UserTypeFlagged) {
			break
		}
	}

	// Convert to JSON for the AI request
	friendDataJSON, err := sonic.Marshal(friendSummaries)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrJSONProcessing, err)
	}

	// Minify JSON to reduce token usage
	friendDataJSON, err = a.minify.Bytes("application/json", friendDataJSON)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrJSONProcessing, err)
	}

	// Configure prompt for friend analysis
	prompt := fmt.Sprintf(FriendUserPrompt, userInfo.Name, string(friendDataJSON))

	// Generate friend analysis using Gemini model
	resp, err := a.friendModel.GenerateContent(context.Background(), genai.Text(prompt))
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrModelResponse, err)
	}

	if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("%w: no response from Gemini", ErrModelResponse)
	}

	// Extract response text from Gemini's response
	responseText := resp.Candidates[0].Content.Parts[0].(genai.Text)

	// Parse Gemini response into FlaggedUser struct
	var flaggedUser FlaggedUser
	err = sonic.Unmarshal([]byte(responseText), &flaggedUser)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrJSONProcessing, err)
	}

	reason := flaggedUser.Reason
	a.logger.Debug("Generated friend network reason",
		zap.String("username", userInfo.Name),
		zap.Int("confirmedFriends", len(confirmedFriends)),
		zap.Int("flaggedFriends", len(flaggedFriends)),
		zap.Int32("totalTokens", currentTokens),
		zap.String("generatedReason", reason))

	return reason, nil
}

// validateFlaggedUsers validates the flagged users against the translated content
// but uses original descriptions when creating validated users. It checks if at least
// 10% of the flagged words are found in the translated content to confirm the AI's findings.
func (a *AIChecker) validateFlaggedUsers(flaggedUsers FlaggedUsers, translatedInfos map[string]*fetcher.Info, originalInfos map[string]*fetcher.Info) (map[uint64]*types.User, []uint64) {
	validatedUsers := make(map[uint64]*types.User)
	var failedValidationIDs []uint64

	for _, flaggedUser := range flaggedUsers.Users {
		normalizedName := utils.NormalizeString(flaggedUser.Name)

		// Check if the flagged user exists in both maps
		translatedInfo, exists := translatedInfos[normalizedName]
		originalInfo, hasOriginal := originalInfos[normalizedName]

		if exists && hasOriginal {
			// Split all flagged content into words
			var allFlaggedWords []string
			for _, content := range flaggedUser.FlaggedContent {
				allFlaggedWords = append(allFlaggedWords, strings.Fields(content)...)
			}

			// Count how many flagged words are found in the translated content
			foundWords := 0
			for _, word := range allFlaggedWords {
				if utils.ContainsNormalized(translatedInfo.Name, word) ||
					(translatedInfo.DisplayName != translatedInfo.Name && utils.ContainsNormalized(translatedInfo.DisplayName, word)) ||
					utils.ContainsNormalized(translatedInfo.Description, word) {
					foundWords++
				}
			}

			// Check if at least 10% of the flagged words are found
			isValid := float64(foundWords) >= 0.1*float64(len(allFlaggedWords))

			// If the flagged user is correct, add it using original info
			if isValid {
				validatedUsers[originalInfo.ID] = &types.User{
					ID:             originalInfo.ID,
					Name:           originalInfo.Name,
					DisplayName:    originalInfo.DisplayName,
					Description:    originalInfo.Description,
					CreatedAt:      originalInfo.CreatedAt,
					Reason:         "AI Analysis: " + flaggedUser.Reason,
					Groups:         originalInfo.Groups.Data,
					Friends:        originalInfo.Friends.Data,
					Games:          originalInfo.Games.Data,
					FollowerCount:  originalInfo.FollowerCount,
					FollowingCount: originalInfo.FollowingCount,
					FlaggedContent: flaggedUser.FlaggedContent,
					Confidence:     flaggedUser.Confidence,
					LastUpdated:    originalInfo.LastUpdated,
				}
			} else {
				failedValidationIDs = append(failedValidationIDs, originalInfo.ID)
				a.logger.Warn("AI flagged content did not pass validation",
					zap.Uint64("userID", originalInfo.ID),
					zap.String("flaggedUsername", flaggedUser.Name),
					zap.String("username", originalInfo.Name),
					zap.String("description", originalInfo.Description),
					zap.Strings("flaggedContent", flaggedUser.FlaggedContent),
					zap.Float64("matchPercentage", float64(foundWords)/float64(len(allFlaggedWords))*100))
			}
		} else {
			a.logger.Warn("AI flagged non-existent user", zap.String("username", flaggedUser.Name))
		}
	}

	return validatedUsers, failedValidationIDs
}

// prepareUserInfos translates user descriptions and maintains maps of both translated
// and original user infos for validation. If translation fails for any description,
// it falls back to using the original content. Returns maps using normalized usernames
// as keys.
func (a *AIChecker) prepareUserInfos(userInfos []*fetcher.Info) (map[string]*fetcher.Info, map[string]*fetcher.Info) {
	// TranslationResult contains the result of translating a user's description.
	type TranslationResult struct {
		UserInfo       *fetcher.Info
		TranslatedDesc string
		Err            error
	}

	var wg sync.WaitGroup
	resultsChan := make(chan TranslationResult, len(userInfos))

	// Create maps for both original and translated infos
	originalInfos := make(map[string]*fetcher.Info)
	translatedInfos := make(map[string]*fetcher.Info)

	// Initialize maps and spawn translation goroutines
	for _, info := range userInfos {
		normalizedName := utils.NormalizeString(info.Name)
		originalInfos[normalizedName] = info

		wg.Add(1)
		go func(info *fetcher.Info) {
			defer wg.Done()

			// Skip empty descriptions
			if info.Description == "" {
				resultsChan <- TranslationResult{
					UserInfo:       info,
					TranslatedDesc: "",
				}
				return
			}

			// Translate the description
			translated, err := a.translator.Translate(
				context.Background(),
				info.Description,
				"auto", // Auto-detect source language
				"en",   // Translate to English
			)

			resultsChan <- TranslationResult{
				UserInfo:       info,
				TranslatedDesc: translated,
				Err:            err,
			}
		}(info)
	}

	// Close results channel when all translations are complete
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// Process results
	for result := range resultsChan {
		normalizedName := utils.NormalizeString(result.UserInfo.Name)
		if result.Err != nil {
			// Use original userInfo if translation fails
			translatedInfos[normalizedName] = result.UserInfo
			a.logger.Error("Translation failed, using original description",
				zap.String("username", result.UserInfo.Name),
				zap.Error(result.Err))
			continue
		}

		// Create new Info with translated description
		translatedInfo := *result.UserInfo
		if translatedInfo.Description != result.TranslatedDesc {
			translatedInfo.Description = result.TranslatedDesc
			a.logger.Debug("Translated description", zap.String("username", translatedInfo.Name))
		}
		translatedInfos[normalizedName] = &translatedInfo
	}

	return translatedInfos, originalInfos
}

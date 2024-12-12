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
	ReviewSystemPrompt = `You are a Roblox moderator analyzing user data for inappropriate content.

Key Rules:
- Flag ANY violations, no matter how subtle
- For 'flaggedContent', copy-paste EXACT text segments from the user's name, display name, or description that violate guidelines
- Never return placeholder text like "string" - only use actual content from the user's profile
- Combine all violations for a user into one entry
- Exclude users without any violations from the response
- Do not flag empty descriptions as they are not violations
- When in doubt, flag the user

Content Categories to Watch For:
- Explicit sexual terms and innuendos
- Body part references
- Hookup solicitation
- Porn references
- Suggestive emojis
- NSFW content
- ERP terms
- Fetish mentions
- Dating requests
Grooming Indicators to Watch For:
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

Predatory Behavior Patterns:
- Love bombing
- Isolation attempts
- Manipulation
- Guilt-tripping tactics
- Camera/mic usage requests
- Private game invites
- Social media contact attempts
- Adult industry references
- Mentions of "studio" in suspicious context
- Requests for private interactions
- References to age preferences
- Suggestive emoji combinations

Confidence Scoring:
+0.6: Explicit violations
+0.4: Clear suggestive content
+0.3: Grooming patterns
+0.2: Subtle violations

Confidence Levels:
High (0.8-1.0): Explicit/multiple violations
Medium (0.4-0.7): Clear patterns
Low (0.0-0.3): Subtle content`

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
+0.6: Multiple confirmed friends with serious violations
+0.4: Multiple flagged friends with clear patterns
+0.2: Mixed confirmed/flagged friends
- +0.1: Each additional same-type violation
- +0.2: Each different violation type

Confidence Levels:
High (0.8-1.0): Strong confirmed networks
Medium (0.4-0.7): Clear patterns with mixed status
Low (0.0-0.3): Limited connections`

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
	userModel  *genai.GenerativeModel
	minify     *minify.M
	translator *translator.Translator
	logger     *zap.Logger
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
							Description: "Exact content that was flagged from the user's profile",
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

	// Create a minifier for JSON optimization
	m := minify.New()
	m.AddFunc("application/json", json.Minify)

	return &AIChecker{
		userModel:  userModel,
		minify:     m,
		translator: translator,
		logger:     logger,
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
	if len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil || len(resp.Candidates[0].Content.Parts) == 0 {
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

// validateFlaggedUsers validates the flagged users against the translated content
// but uses original descriptions when creating validated users. It checks if at least
// 30% of the flagged words are found in the translated content to confirm the AI's findings.
func (a *AIChecker) validateFlaggedUsers(flaggedUsers FlaggedUsers, translatedInfos map[string]*fetcher.Info, originalInfos map[string]*fetcher.Info) (map[uint64]*types.User, []uint64) {
	validatedUsers := make(map[uint64]*types.User)
	var failedValidationIDs []uint64

	for _, flaggedUser := range flaggedUsers.Users {
		// Check if the flagged user exists in both maps
		translatedInfo, exists := translatedInfos[flaggedUser.Name]
		originalInfo, hasOriginal := originalInfos[flaggedUser.Name]

		if exists && hasOriginal && flaggedUser.Confidence > 0 {
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

			// Check if at least 30% of the flagged words are found
			isValid := float64(foundWords) >= 0.3*float64(len(allFlaggedWords))

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
		originalInfos[info.Name] = info

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
		if result.Err != nil {
			// Use original userInfo if translation fails
			translatedInfos[result.UserInfo.Name] = result.UserInfo
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
		translatedInfos[result.UserInfo.Name] = &translatedInfo
	}

	return translatedInfos, originalInfos
}

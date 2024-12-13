package ai

import (
	"context"
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
	ReviewSystemPrompt = `You are a Roblox moderator focused on detecting predatory behavior and inappropriate content targeting minors.

You will receive a list of user profiles in JSON format. Each profile contains:
- Username
- Display name (if different from username)
- Profile description/bio

Analyze each profile and identify users engaging in inappropriate behavior. Return a list of users that violate or potentially violate the guidelines, including:
- The exact username
- Clear explanation of violations found
- Exact quotes of the concerning content
- Confidence level of assessment

Guidelines for flagging:
- Flag explicit or subtle violations and predatory patterns
- When in doubt about violations, flag with lower confidence (0.1-0.4)
- It is better to have false positives than miss predators
- Do not add users with no violations to the list
- Do not flag empty descriptions

Look for predatory behavior:
- Grooming attempts and manipulation:
  * Befriending minors with bad intent
  * Love bombing and excessive compliments
  * Offering gifts or special privileges
  * Guilt-tripping tactics
  * Isolation attempts
  * Building trust through manipulation

- Sexual content and inappropriate requests:
  * Sexual solicitation or innuendo
  * Explicit sexual terms
  * Body part references
  * Porn or NSFW content
  * ERP (erotic roleplay) terms
  * Fetish mentions
  * Roleplay requests
  * Dating or hookup requests
  * Inappropriate emoji or symbol combinations
  * Asking for "fun" or to "use me"
  * References to "zero consent" or "limitless"
  * Suspicious use of "follows TOS" or similar disclaimers
  * References to "cons" or adult conventions
  * Suggestive size references ("big", "massive", "huge" + context)
  * Double meaning phrases about "packages" or "things"
  * Goddess/master/dom references in suspicious context
  * References to "studio" or "game"

- Age-related red flags:
  * Age questions or preferences
  * References to being "young" or "older"
  * Age gap mentions
  * Age + roleplay combinations
  * Dating requests with age context

- Private contact attempts:
  * Requests for private meetings
  * Off-platform chat attempts
  * Social media contact requests
  * Camera/mic usage requests
  * Private game or chat invites
  * Photo requests
  * Personal information seeking

- Suspicious patterns:
  * Coded language for inappropriate activities
  * Non-consensual references
  * Exploitation/harassment
  * Adult industry references
  * Suggestive emoji or symbol patterns
  * Disclaimers that seem to hide inappropriate intent
  * Vague offers of "fun" or "good times"

Confidence level grading:
1.0: Multiple explicit violations
0.8: Single explicit violation or clear predatory pattern
0.6: Clear pattern of concerning behavior
0.4: Multiple suspicious indicators
0.2: One or a few concerning indicators`
)

// MaxFriendDataTokens is the maximum number of tokens allowed for friend data.
const MaxFriendDataTokens = 400

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

// UserAnalyzer handles AI-based content analysis using Gemini models.
type UserAnalyzer struct {
	userModel  *genai.GenerativeModel
	minify     *minify.M
	translator *translator.Translator
	logger     *zap.Logger
}

// NewUserAnalyzer creates an UserAnalyzer with separate models for user and friend analysis.
func NewUserAnalyzer(app *setup.App, translator *translator.Translator, logger *zap.Logger) *UserAnalyzer {
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
							Description: "Clear explanation of why the user was flagged, must describe violations found in their profile",
						},
						"flaggedContent": {
							Type: genai.TypeArray,
							Items: &genai.Schema{
								Type: genai.TypeString,
							},
							Description: "Exact content that was flagged from the user's profile, must exist in original text",
						},
						"confidence": {
							Type:        genai.TypeNumber,
							Description: `Confidence level of moderator's assessment based on severity and number of violations found`,
						},
					},
					Required: []string{"name", "reason", "flaggedContent", "confidence"},
				},
				Description: "Array of users with clear violations. Leave empty if no violations found in any profiles",
			},
		},
		Required: []string{"users"},
	}
	userTemp := float32(0.0)
	userModel.Temperature = &userTemp

	// Create a minifier for JSON optimization
	m := minify.New()
	m.AddFunc("application/json", json.Minify)

	return &UserAnalyzer{
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
func (a *UserAnalyzer) ProcessUsers(userInfos []*fetcher.Info) (map[uint64]*types.User, []uint64, error) {
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
func (a *UserAnalyzer) validateFlaggedUsers(flaggedUsers FlaggedUsers, translatedInfos map[string]*fetcher.Info, originalInfos map[string]*fetcher.Info) (map[uint64]*types.User, []uint64) {
	validatedUsers := make(map[uint64]*types.User)
	var failedValidationIDs []uint64

	for _, flaggedUser := range flaggedUsers.Users {
		translatedInfo, exists := translatedInfos[flaggedUser.Name]
		originalInfo, hasOriginal := originalInfos[flaggedUser.Name]

		// Check if the flagged user exists in both maps
		if !exists && !hasOriginal {
			a.logger.Warn("AI flagged non-existent user", zap.String("username", flaggedUser.Name))
			continue
		}

		// Validate confidence level
		if flaggedUser.Confidence < 0.2 || flaggedUser.Confidence > 1.0 {
			a.logger.Debug("AI flagged user with invalid confidence",
				zap.String("username", flaggedUser.Name),
				zap.Float64("confidence", flaggedUser.Confidence))
			continue
		}

		// Validate that flagged content exists
		if len(flaggedUser.FlaggedContent) == 0 {
			a.logger.Debug("AI flagged user without specific content",
				zap.String("username", flaggedUser.Name))
			continue
		}

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
	}

	return validatedUsers, failedValidationIDs
}

// prepareUserInfos translates user descriptions and maintains maps of both translated
// and original user infos for validation. If translation fails for any description,
// it falls back to using the original content. Returns maps using normalized usernames
// as keys.
func (a *UserAnalyzer) prepareUserInfos(userInfos []*fetcher.Info) (map[string]*fetcher.Info, map[string]*fetcher.Info) {
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

package ai

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"unicode"

	"github.com/bytedance/sonic"
	"github.com/google/generative-ai-go/genai"
	"github.com/robalyx/rotector/internal/common/client/fetcher"
	"github.com/robalyx/rotector/internal/common/setup"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/robalyx/rotector/internal/common/translator"
	"github.com/robalyx/rotector/internal/common/utils"
	"github.com/tdewolff/minify/v2"
	"github.com/tdewolff/minify/v2/json"
	"go.uber.org/zap"
	"golang.org/x/sync/semaphore"
	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

const (
	// UserSystemPrompt provides detailed instructions to the AI model for analyzing user content.
	UserSystemPrompt = `You are a Roblox moderator focused on detecting PREDATORY BEHAVIOR and INAPPROPRIATE CONTENT targeting minors.

You will receive a list of user profiles in JSON format. Each profile contains:
- Username
- Display name (if different from username)
- Profile description/bio

Analyze each profile and identify users engaging in PREDATORY BEHAVIOR. For each profile, return:
- username: The exact username provided
- reason: Clear explanation of violations found in one sentence. Use exactly "NO_VIOLATIONS" if no clear concerns found
- flaggedContent: Exact quotes of the concerning content
- confidence: Level (0.0-1.0) based on severity
  * Use 0.0 for profiles with no clear violations
  * Use 0.1-1.0 ONLY for profiles with predatory elements

Confidence Level Guide:
- 0.0: No predatory elements detected
- 0.1-0.3: Subtle concerning red flags requiring investigation
- 0.4-0.6: Clear inappropriate content or behavior
- 0.7-0.8: Strong indicators of predatory behavior
- 0.9-1.0: Explicit predatory intent or grooming attempts

STRICT RULES:
1. DO NOT flag profiles for:
   - Simple greetings (Hi, Hello, Hey)
   - Casual farewells (Bye, See ya)
   - Simple responses (Yes, No, OK)
   - Empty/placeholder descriptions
2. DO NOT include usernames in your reason
3. DO NOT add users with no violations to the response
4. DO NOT repeat the same content in flaggedContent array
5. DO NOT flag empty descriptions
6. Flag profiles showing MULTIPLE SUBTLE RED FLAGS even if individual elements seem innocent

Look for predatory behavior:
- Grooming attempts and manipulation:
  * Befriending minors with bad intent
  * Building trust through manipulation
  * Love bombing and excessive compliments (e.g., "good girl", "good boy")
  * Exploiting vulnerabilities (e.g. "vulnerable", "needy", "lonely")
  * Offering gifts or special privileges
  * Using phrases like MDNI (minors do not interact) to hide predatory intent
  * Unnecessary declarations of following TOS/Rules (e.g. "TOS follower", "follows TOS")
  * Coded language for inappropriate activities
  * Vague offers of "fun" or "good times" (e.g. "add me if you got games")
  * Adult industry references
  * References to adult fandoms or communities (e.g. "furry", "bara", "futa"/"fmta"/"fvta")
  * Offering Robux, limited items, or game passes as incentives
  * Requests to join "exclusive" Roblox groups/clans
  * Requests for "bottom" or "top" role preferences
  * Misspelled/bypassed words (e.g. "stinky", "gas", "poop", "pee", "fart", "loads given")
  * Phonetic replacements for inappropriate terms
  * Unicode character substitutions

- Sexual content and inappropriate requests:
  * Explicit sexual terms
  * Sexual solicitation or innuendo
  * Body part references
  * Porn or NSFW content
  * ERP (erotic roleplay) terms
  * Fetish mentions (e.g. "giant"/"giantess" sizeplay, scatological terms)
  * Dating or hookup requests
  * Roleplay requests
  * Gooner references (internet slang for compulsive behavior)
  * Specifying "literate" or "illiterate" partners
  * Suggestive size references (e.g. "big", "massive", "huge", "giant", "giantess")
  * References to "Game", "condo", "con", "studio" 
  * Phrases like "no condo", "no studio", "games only", etc. (often used to hide intent)
  * References to being "young" or "older" (especially underage mentions)
  * Double meaning phrases about "packages" or "things"
  * "Trading" with sexual implications or off-platform exchanges (e.g. "trade pics", "special trades")
  * Goddess/master/dom references
  * Exploitative "adopt me" scenarios or family roleplay
  * Suggestive emoji or symbol patterns
  * Inappropriate emoji or symbol combinations
  * Asking for "fun" or to "use me"
  * References to "zero consent" or "limitless"
  * References to "bulls"
  * Non-consensual references
  * Exploitation/harassment references

- Private contact attempts:
  * Sharing condo game codes/locations
  * "Test my game" requests with private access
  * Friend requests with age-related comments ("I need young friends")
  * Friend requests with sexualized context ("friends with benefits")
  * Friend requests with trade coercion ("friend me for free Robux")
  * Off-platform chat attempts
  * References to "blue app" or "blue user" (Telegram)
  * References to private servers, rooms, or games
  * Requests to "add me" combined with inappropriate context`
)

var ErrBatchProcessing = errors.New("batch processing errors")

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
	userModel   *genai.GenerativeModel
	minify      *minify.M
	translator  *translator.Translator
	analysisSem *semaphore.Weighted
	batchSize   int
	logger      *zap.Logger
}

// NewUserAnalyzer creates an UserAnalyzer with separate models for user and friend analysis.
func NewUserAnalyzer(app *setup.App, translator *translator.Translator, logger *zap.Logger) *UserAnalyzer {
	// Create user analysis model
	userModel := app.GenAIClient.GenerativeModel(app.Config.Common.GeminiAI.Model)
	userModel.SystemInstruction = genai.NewUserContent(genai.Text(UserSystemPrompt))
	userModel.ResponseMIMEType = ApplicationJSON
	userModel.ResponseSchema = &genai.Schema{
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
	userModel.Temperature = utils.Ptr(float32(0.1))
	userModel.TopP = utils.Ptr(float32(0.5))
	userModel.TopK = utils.Ptr(int32(10))
	m := minify.New()
	m.AddFunc(ApplicationJSON, json.Minify)

	return &UserAnalyzer{
		userModel:   userModel,
		minify:      m,
		translator:  translator,
		analysisSem: semaphore.NewWeighted(int64(app.Config.Worker.BatchSizes.UserAnalysis)),
		batchSize:   app.Config.Worker.BatchSizes.UserAnalysisBatch,
		logger:      logger,
	}
}

// ProcessUsers sends user information to a Gemini model for analysis after translating descriptions.
// Returns IDs of users that failed validation for retry.
func (a *UserAnalyzer) ProcessUsers(userInfos []*fetcher.Info, flaggedUsers map[uint64]*types.User) ([]uint64, error) {
	numBatches := (len(userInfos) + a.batchSize - 1) / a.batchSize

	type batchResult struct {
		failedIDs []uint64
		err       error
	}

	// Process batches concurrently
	results := make(chan batchResult, numBatches)
	var wg sync.WaitGroup
	var mu sync.Mutex

	for i := range numBatches {
		wg.Add(1)
		go func(offset int) {
			defer wg.Done()

			start := offset * a.batchSize
			end := start + a.batchSize
			if end > len(userInfos) {
				end = len(userInfos)
			}

			// Acquire semaphore before making AI request
			if err := a.analysisSem.Acquire(context.Background(), 1); err != nil {
				results <- batchResult{
					failedIDs: getUserIDs(userInfos[start:end]),
					err:       fmt.Errorf("failed to acquire semaphore: %w", err),
				}
				return
			}
			defer a.analysisSem.Release(1)

			// Process batch
			failedIDs, err := a.processBatch(userInfos[start:end], flaggedUsers, &mu)
			results <- batchResult{failedIDs: failedIDs, err: err}
		}(i)
	}

	// Wait for all batches to complete
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results
	var allFailedIDs []uint64
	var errors []error

	for result := range results {
		if result.err != nil {
			errors = append(errors, result.err)
		}
		allFailedIDs = append(allFailedIDs, result.failedIDs...)
	}

	if len(errors) > 0 {
		return allFailedIDs, fmt.Errorf("%w: %v", ErrBatchProcessing, errors)
	}

	a.logger.Info("Received AI user analysis",
		zap.Int("totalUsers", len(userInfos)),
		zap.Int("flaggedUsers", len(flaggedUsers)))

	return allFailedIDs, nil
}

// processBatch handles a single batch of users.
func (a *UserAnalyzer) processBatch(userInfos []*fetcher.Info, flaggedUsers map[uint64]*types.User, mu *sync.Mutex) ([]uint64, error) {
	// Translate all descriptions concurrently
	translatedInfos, originalInfos := a.prepareUserInfos(userInfos)

	// Create a struct for user summaries for AI analysis
	type UserSummary struct {
		Name        string `json:"name"`
		DisplayName string `json:"displayName,omitempty"`
		Description string `json:"description"`
	}

	// Convert map to slice for AI request
	userInfosWithoutID := make([]UserSummary, 0, len(translatedInfos))
	for _, userInfo := range translatedInfos {
		summary := UserSummary{
			Name: userInfo.Name,
		}

		// Only include display name if it's different from the username
		if userInfo.DisplayName != userInfo.Name {
			summary.DisplayName = userInfo.DisplayName
		}

		// Replace empty descriptions with placeholder
		description := userInfo.Description
		if description == "" {
			description = "[Empty profile]"
		}
		summary.Description = description

		userInfosWithoutID = append(userInfosWithoutID, summary)
	}

	// Minify JSON to reduce token usage
	userInfoJSON, err := sonic.Marshal(userInfosWithoutID)
	if err != nil {
		return getUserIDs(userInfos), fmt.Errorf("%w: %w", ErrJSONProcessing, err)
	}

	userInfoJSON, err = a.minify.Bytes(ApplicationJSON, userInfoJSON)
	if err != nil {
		return getUserIDs(userInfos), fmt.Errorf("%w: %w", ErrJSONProcessing, err)
	}

	// Generate content and parse response using Gemini model with retry
	flaggedResults, err := withRetry(context.Background(), func() (*FlaggedUsers, error) {
		resp, err := a.userModel.GenerateContent(context.Background(), genai.Text(string(userInfoJSON)))
		if err != nil {
			return nil, fmt.Errorf("gemini API error: %w", err)
		}

		// Check for empty response
		if len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil || len(resp.Candidates[0].Content.Parts) == 0 {
			return nil, fmt.Errorf("%w: no response from Gemini", ErrModelResponse)
		}

		// Extract and parse response
		responseText := resp.Candidates[0].Content.Parts[0].(genai.Text)
		var result FlaggedUsers
		if err := sonic.Unmarshal([]byte(responseText), &result); err != nil {
			return nil, fmt.Errorf("JSON unmarshal error: %w", err)
		}

		return &result, nil
	})
	if err != nil {
		return getUserIDs(userInfos), fmt.Errorf("%w: %w", ErrModelResponse, err)
	}

	// Validate AI responses and update flaggedUsers map
	failedValidationIDs := a.validateAndUpdateFlaggedUsers(flaggedResults, translatedInfos, originalInfos, flaggedUsers, mu)

	return failedValidationIDs, nil
}

// validateAndUpdateFlaggedUsers validates the flagged users and updates the flaggedUsers map.
func (a *UserAnalyzer) validateAndUpdateFlaggedUsers(flaggedResults *FlaggedUsers, translatedInfos, originalInfos map[string]*fetcher.Info, flaggedUsers map[uint64]*types.User, mu *sync.Mutex) []uint64 {
	var failedValidationIDs []uint64
	normalizer := transform.Chain(
		norm.NFKD,                             // Decompose with compatibility decomposition
		runes.Remove(runes.In(unicode.Mn)),    // Remove non-spacing marks
		runes.Remove(runes.In(unicode.P)),     // Remove punctuation
		runes.Map(unicode.ToLower),            // Convert to lowercase before normalization
		norm.NFKC,                             // Normalize with compatibility composition
		runes.Remove(runes.In(unicode.Space)), // Remove spaces
	)

	for _, flaggedUser := range flaggedResults.Users {
		translatedInfo, exists := translatedInfos[flaggedUser.Name]
		originalInfo, hasOriginal := originalInfos[flaggedUser.Name]

		// Check if the flagged user exists in both maps
		if !exists && !hasOriginal {
			a.logger.Warn("AI flagged non-existent user", zap.String("username", flaggedUser.Name))
			continue
		}

		// Skip results with no violations
		if flaggedUser.Reason == "NO_VIOLATIONS" {
			continue
		}

		// Validate confidence level
		if flaggedUser.Confidence < 0.1 || flaggedUser.Confidence > 1.0 {
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

		// Validate flagged content against user texts
		isValid := utils.ValidateFlaggedWords(flaggedUser.FlaggedContent,
			normalizer,
			translatedInfo.Name,
			translatedInfo.DisplayName,
			translatedInfo.Description)

		// If the flagged user is valid, update the flaggedUsers map
		if isValid {
			mu.Lock()
			if existingUser, ok := flaggedUsers[originalInfo.ID]; ok {
				// Combine reasons and update confidence
				existingUser.Reason = fmt.Sprintf("%s\n\nAI Analysis: %s", existingUser.Reason, flaggedUser.Reason)
				existingUser.Confidence = 1.0
				existingUser.FlaggedContent = flaggedUser.FlaggedContent
			} else {
				flaggedUsers[originalInfo.ID] = &types.User{
					ID:                  originalInfo.ID,
					Name:                originalInfo.Name,
					DisplayName:         originalInfo.DisplayName,
					Description:         originalInfo.Description,
					CreatedAt:           originalInfo.CreatedAt,
					Reason:              "AI Analysis: " + flaggedUser.Reason,
					Groups:              originalInfo.Groups.Data,
					Friends:             originalInfo.Friends.Data,
					Games:               originalInfo.Games.Data,
					Outfits:             originalInfo.Outfits.Data,
					FollowerCount:       originalInfo.FollowerCount,
					FollowingCount:      originalInfo.FollowingCount,
					FlaggedContent:      flaggedUser.FlaggedContent,
					Confidence:          flaggedUser.Confidence,
					LastUpdated:         originalInfo.LastUpdated,
					LastBanCheck:        originalInfo.LastBanCheck,
					ThumbnailURL:        originalInfo.ThumbnailURL,
					LastThumbnailUpdate: originalInfo.LastThumbnailUpdate,
				}
			}
			mu.Unlock()
		} else {
			failedValidationIDs = append(failedValidationIDs, originalInfo.ID)
			a.logger.Warn("AI flagged content did not pass validation",
				zap.Uint64("userID", originalInfo.ID),
				zap.String("flaggedUsername", flaggedUser.Name),
				zap.String("username", originalInfo.Name),
				zap.String("description", originalInfo.Description),
				zap.Strings("flaggedContent", flaggedUser.FlaggedContent))
		}
	}

	return failedValidationIDs
}

// prepareUserInfos translates user descriptions for different languages and encodings.
// Returns maps of both translated and original user infos for validation.
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

			// Translate the description with retry
			translated, err := withRetry(context.Background(), func() (string, error) {
				return a.translator.Translate(
					context.Background(),
					info.Description,
					"auto", // Auto-detect source language
					"en",   // Translate to English
				)
			})

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

// getUserIDs extracts user IDs from a slice of user infos.
func getUserIDs(userInfos []*fetcher.Info) []uint64 {
	ids := make([]uint64, len(userInfos))
	for i, info := range userInfos {
		ids[i] = info.ID
	}
	return ids
}

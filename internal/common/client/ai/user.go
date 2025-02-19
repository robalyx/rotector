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
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
	"github.com/robalyx/rotector/internal/common/translator"
	"github.com/robalyx/rotector/internal/common/utils"
	"github.com/sourcegraph/conc/pool"
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
	UserSystemPrompt = `You are a Roblox content moderator specializing in detecting predatory behavior targeting minors.

Input format:
{
  "users": [
    {
      "name": "username",
      "displayName": "optional display name",
      "description": "profile description"
    }
  ]
}

Output format:
{
  "users": [
    {
      "name": "username",
      "reason": "Clear explanation in one sentence",
      "flaggedContent": ["exact quote 1", "exact quote 2"],
      "confidence": 0.0-1.0
    }
  ]
}

Confidence levels:
0.0: No predatory elements
0.1-0.3: Subtle predatory elements
0.4-0.6: Clear inappropriate content  
0.7-0.8: Strong predatory indicators
0.9-1.0: Explicit predatory intent

Key rules:
1. Return ONLY users with violations
2. Use "the user" or "this account" instead of usernames
3. Include exact quotes in flaggedContent
4. Set NO_VIOLATIONS if no concerns found
5. Skip empty descriptions

Look for:
- Coded language for inappropriate activities
- Leading phrases implying secrecy
- Inappropriate game/chat/studio invitations
- Condo/con references
- "Exclusive" group invitations
- Sexual content or innuendo
- Suspicious direct messaging demands
- Sexual solicitation
- Erotic roleplay (ERP)
- Age-inappropriate dating content
- Exploitative adoption scenarios
- Non-consensual references
- Friend requests with inappropriate context
- Claims of following TOS/rules to avoid detection
- Mentions of Telegram/"blue app" for off-platform chat
- Age-restricted invitations
- Modified app references
- Inappropriate roleplay requests
- Control dynamics
- Service-oriented grooming
- Gender-specific minor targeting
- Ownership/dominance references
- Boundary violations
- Exploitation references
- Fetish content
- Bypassed inappropriate terms
- Adult community references
- Suggestive size references
- Inappropriate trading
- Degradation terms

Ignore:
- Simple greetings/farewells
- Basic responses
- Empty descriptions
- Emoji usage
- Gaming preferences
- Non-inappropriate content
- Non-sexual roleplay
- General social interactions
- Age mentions
- Compliments on outfits/avatars
- Any follow or friend making requests
- Advertisements to join games, communities, channels, tournaments, etc.
- Off-platform handles without inappropriate context
- Friend making attempts without inappropriate context
- Gender identity expression
- Bypass of appropriate terms
- Self-harm or suicide-related content
- Violence, gore, or disturbing content
- Sharing of personal information`

	// UserRequestPrompt provides a reminder to follow system guidelines for user analysis.
	UserRequestPrompt = `Analyze these user profiles for predatory content targeting minors.

Remember:
1. Return only users with clear violations
2. Use "the user"/"this account" instead of usernames
3. Include exact quotes as evidence
4. Follow confidence level guide strictly

Profiles to analyze:
`
)

var ErrBatchProcessing = errors.New("batch processing errors")

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
	userModel.Temperature = utils.Ptr(float32(0.2))
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

// ProcessUsers analyzes user content for a batch of users.
func (a *UserAnalyzer) ProcessUsers(userInfos []*fetcher.Info, flaggedUsers map[uint64]*types.User) {
	// Track counts before processing
	existingFlags := len(flaggedUsers)
	numBatches := (len(userInfos) + a.batchSize - 1) / a.batchSize

	// Process batches concurrently
	var (
		p  = pool.New().WithContext(context.Background())
		mu sync.Mutex
	)

	for i := range numBatches {
		start := i * a.batchSize
		end := start + a.batchSize
		if end > len(userInfos) {
			end = len(userInfos)
		}

		infoBatch := userInfos[start:end]
		p.Go(func(ctx context.Context) error {
			// Acquire semaphore before making AI request
			if err := a.analysisSem.Acquire(ctx, 1); err != nil {
				return fmt.Errorf("failed to acquire semaphore: %w", err)
			}
			defer a.analysisSem.Release(1)

			// Process batch
			if err := a.processBatch(ctx, infoBatch, flaggedUsers, &mu); err != nil {
				a.logger.Error("Failed to process batch",
					zap.Error(err),
					zap.Int("batchStart", start),
					zap.Int("batchEnd", end))
				return err
			}
			return nil
		})
	}

	// Wait for all batches to complete
	if err := p.Wait(); err != nil {
		a.logger.Error("Error during user analysis", zap.Error(err))
		return
	}

	a.logger.Info("Received AI user analysis",
		zap.Int("totalUsers", len(userInfos)),
		zap.Int("newFlags", len(flaggedUsers)-existingFlags))
}

// processBatch handles a single batch of users.
func (a *UserAnalyzer) processBatch(ctx context.Context, userInfos []*fetcher.Info, flaggedUsers map[uint64]*types.User, mu *sync.Mutex) error {
	// Translate all descriptions concurrently
	translatedInfos, originalInfos := a.prepareUserInfos(ctx, userInfos)

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
		return fmt.Errorf("%w: %w", ErrJSONProcessing, err)
	}

	userInfoJSON, err = a.minify.Bytes(ApplicationJSON, userInfoJSON)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrJSONProcessing, err)
	}

	// Prepare request prompt with user info
	requestPrompt := UserRequestPrompt + string(userInfoJSON)

	// Generate content and parse response using Gemini model with retry
	flaggedResults, err := withRetry(ctx, func() (*FlaggedUsers, error) {
		resp, err := a.userModel.GenerateContent(ctx, genai.Text(requestPrompt))
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
		return fmt.Errorf("%w: %w", ErrModelResponse, err)
	}

	// Validate AI responses
	a.validateAndUpdateFlaggedUsers(flaggedResults, translatedInfos, originalInfos, flaggedUsers, mu)

	return nil
}

// validateAndUpdateFlaggedUsers validates the flagged users and updates the flaggedUsers map.
func (a *UserAnalyzer) validateAndUpdateFlaggedUsers(flaggedResults *FlaggedUsers, translatedInfos, originalInfos map[string]*fetcher.Info, flaggedUsers map[uint64]*types.User, mu *sync.Mutex) {
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
				existingUser.Reasons[enum.ReasonTypeUser] = &types.Reason{
					Message:    flaggedUser.Reason,
					Confidence: flaggedUser.Confidence,
					Evidence:   flaggedUser.FlaggedContent,
				}
			} else {
				flaggedUsers[originalInfo.ID] = &types.User{
					ID:          originalInfo.ID,
					Name:        originalInfo.Name,
					DisplayName: originalInfo.DisplayName,
					Description: originalInfo.Description,
					CreatedAt:   originalInfo.CreatedAt,
					Reasons: types.Reasons{
						enum.ReasonTypeUser: &types.Reason{
							Message:    flaggedUser.Reason,
							Confidence: flaggedUser.Confidence,
							Evidence:   flaggedUser.FlaggedContent,
						},
					},
					Groups:              originalInfo.Groups.Data,
					Friends:             originalInfo.Friends.Data,
					Games:               originalInfo.Games.Data,
					Outfits:             originalInfo.Outfits.Data,
					LastUpdated:         originalInfo.LastUpdated,
					LastBanCheck:        originalInfo.LastBanCheck,
					ThumbnailURL:        originalInfo.ThumbnailURL,
					LastThumbnailUpdate: originalInfo.LastThumbnailUpdate,
				}
			}
			mu.Unlock()
		} else {
			a.logger.Warn("AI flagged content did not pass validation",
				zap.Uint64("userID", originalInfo.ID),
				zap.String("flaggedUsername", flaggedUser.Name),
				zap.String("username", originalInfo.Name),
				zap.String("description", originalInfo.Description),
				zap.Strings("flaggedContent", flaggedUser.FlaggedContent))
		}
	}
}

// prepareUserInfos translates user descriptions for different languages and encodings.
// Returns maps of both translated and original user infos for validation.
func (a *UserAnalyzer) prepareUserInfos(ctx context.Context, userInfos []*fetcher.Info) (map[string]*fetcher.Info, map[string]*fetcher.Info) {
	var (
		originalInfos   = make(map[string]*fetcher.Info)
		translatedInfos = make(map[string]*fetcher.Info)
		p               = pool.New().WithContext(ctx)
		mu              sync.Mutex
	)

	// Initialize maps and spawn translation goroutines
	for _, info := range userInfos {
		originalInfos[info.Name] = info

		p.Go(func(ctx context.Context) error {
			// Skip empty descriptions
			if info.Description == "" {
				mu.Lock()
				translatedInfos[info.Name] = info
				mu.Unlock()
				return nil
			}

			// Translate the description with retry
			translated, err := withRetry(ctx, func() (string, error) {
				return a.translator.Translate(
					ctx,
					info.Description,
					"auto", // Auto-detect source language
					"en",   // Translate to English
				)
			})
			if err != nil {
				// Use original userInfo if translation fails
				mu.Lock()
				translatedInfos[info.Name] = info
				mu.Unlock()
				a.logger.Error("Translation failed, using original description",
					zap.String("username", info.Name),
					zap.Error(err))
				return nil
			}

			// Create new Info with translated description
			translatedInfo := *info
			if translatedInfo.Description != translated {
				translatedInfo.Description = translated
				a.logger.Debug("Translated description", zap.String("username", info.Name))
			}
			mu.Lock()
			translatedInfos[info.Name] = &translatedInfo
			mu.Unlock()
			return nil
		})
	}

	// Wait for all translations to complete
	if err := p.Wait(); err != nil {
		a.logger.Error("Error during translations", zap.Error(err))
	}

	return translatedInfos, originalInfos
}

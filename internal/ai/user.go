package ai

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/bytedance/sonic"
	"github.com/openai/openai-go"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/database/types/enum"
	"github.com/robalyx/rotector/internal/setup"
	"github.com/robalyx/rotector/internal/translator"
	"github.com/robalyx/rotector/pkg/utils"
	"github.com/sourcegraph/conc/pool"
	"github.com/tdewolff/minify/v2"
	"github.com/tdewolff/minify/v2/json"
	"go.uber.org/zap"
	"golang.org/x/sync/semaphore"
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
      "confidence": 0.0-1.0,
      "hasSocials": true/false
    }
  ]
}

Key rules:
1. Return ALL users that either have violations OR contain social media links
2. Use "the user" or "this account" instead of usernames
3. Include exact quotes in flaggedContent
4. Set reason to NO_VIOLATIONS if no violations found
5. Skip empty descriptions
6. Set hasSocials to true if description contains any social media handles/links
7. If user has no violations but has socials, only include name and hasSocials fields
8. If user is stating their social media, it is not a violation
9. DO NOT flag users for being potential victims
10. ONLY flag users exhibiting predatory or inappropriate behavior
11. Flag usernames/display names even if the description is empty. The name itself can be sufficient evidence for flagging.

Confidence levels (only for inappropriate content):
0.0: No predatory elements
0.1-0.3: Subtle predatory elements
0.4-0.6: Clear inappropriate content  
0.7-0.8: Strong predatory indicators
0.9-1.0: Explicit predatory intent

Look for inappropriate content FROM PREDATORS:
- Coded language implying inappropriate activities
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
- Caesar cipher (ROT13 and other rotations)

Look for inappropriate usernames/display names suggesting PREDATORY intentions:
- Implying ownership, control, or subservience
- Suggesting sexual availability or soliciting inappropriate interactions 
- Using pet names or diminutives suggestively
- Targeting minors or specific genders inappropriately
- Using coded language or suggestive terms related to inappropriate acts
- Hinting at exploitation or predatory activities
- References to adult content platforms or services
- Deliberately misspelled inappropriate terms
- Combinations of innocent words that create suggestive meanings
- Animal terms used in suggestive contexts
- Possessive terms combined with suggestive words

When flagging inappropriate names:
- Set confidence based on how explicit/obvious the inappropriate content is
- Include the full username/display name as flagged content
- Set reason to clearly explain why the name is inappropriate
- Consider combinations of words that together create inappropriate meanings

Ignore:
- Simple greetings/farewells
- Basic responses
- Empty descriptions
- Emoji usage
- Gaming preferences
- Non-inappropriate content
- Non-sexual roleplay
- General social interactions
- Social media usernames/handles/tags/servers
- Age mentions or bypasses
- Compliments on outfits/avatars
- Any follow or friend making requests
- Advertisements to join games, communities, channels, or tournaments
- Off-platform handles without inappropriate context
- Friend making attempts without inappropriate context
- Gender identity expression
- Bypass of appropriate terms
- Self-harm or suicide-related content
- Violence, gore, or disturbing content
- Sharing of personal information`

	// UserRequestPrompt provides a reminder to follow system guidelines for user analysis.
	UserRequestPrompt = `Analyze these user profiles for predatory content and social media links.

Remember:
1. Return ALL users that either have violations OR contain social media links
2. Use "the user"/"this account" instead of usernames
3. Include exact quotes as evidence for violations
4. Follow confidence level guide strictly
5. Always set hasSocials field accurately
6. For users with only social media links (no violations), include only name and hasSocials fields

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
	HasSocials     bool     `json:"hasSocials"`
}

// UserAnalyzer handles AI-based content analysis using OpenAI models.
type UserAnalyzer struct {
	openAIClient *openai.Client
	minify       *minify.M
	translator   *translator.Translator
	analysisSem  *semaphore.Weighted
	logger       *zap.Logger
	model        string
	batchSize    int
}

// UserAnalysisSchema is the JSON schema for the user analysis response.
var UserAnalysisSchema = utils.GenerateSchema[FlaggedUsers]()

// NewUserAnalyzer creates an UserAnalyzer with separate models for user and friend analysis.
func NewUserAnalyzer(app *setup.App, translator *translator.Translator, logger *zap.Logger) *UserAnalyzer {
	// Create a minifier for JSON optimization
	m := minify.New()
	m.AddFunc(ApplicationJSON, json.Minify)

	return &UserAnalyzer{
		openAIClient: app.OpenAIClient,
		minify:       m,
		translator:   translator,
		analysisSem:  semaphore.NewWeighted(int64(app.Config.Worker.BatchSizes.UserAnalysis)),
		logger:       logger.Named("ai_user"),
		model:        app.Config.Common.OpenAI.Model,
		batchSize:    app.Config.Worker.BatchSizes.UserAnalysisBatch,
	}
}

// ProcessUsers analyzes user content for a batch of users.
func (a *UserAnalyzer) ProcessUsers(userInfos []*types.User, reasonsMap map[uint64]types.Reasons[enum.UserReasonType]) {
	// Track counts before processing
	existingFlags := len(reasonsMap)
	numBatches := (len(userInfos) + a.batchSize - 1) / a.batchSize

	// Process batches concurrently
	var (
		ctx = context.Background()
		p   = pool.New().WithContext(ctx)
		mu  sync.Mutex
	)

	for i := range numBatches {
		start := i * a.batchSize
		end := min(start+a.batchSize, len(userInfos))

		infoBatch := userInfos[start:end]
		p.Go(func(ctx context.Context) error {
			// Acquire semaphore before making AI request
			if err := a.analysisSem.Acquire(ctx, 1); err != nil {
				return fmt.Errorf("failed to acquire semaphore: %w", err)
			}
			defer a.analysisSem.Release(1)

			// Process batch
			if err := a.processBatch(ctx, infoBatch, reasonsMap, &mu); err != nil {
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
		zap.Int("newFlags", len(reasonsMap)-existingFlags))
}

// processBatch handles a single batch of users.
func (a *UserAnalyzer) processBatch(
	ctx context.Context, userInfos []*types.User, reasonsMap map[uint64]types.Reasons[enum.UserReasonType], mu *sync.Mutex,
) error {
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

	// Generate user analysis
	resp, err := a.openAIClient.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(UserSystemPrompt),
			openai.UserMessage(requestPrompt),
		},
		ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONSchema: &openai.ResponseFormatJSONSchemaParam{
				JSONSchema: openai.ResponseFormatJSONSchemaJSONSchemaParam{
					Name:        "userAnalysis",
					Description: openai.String("Analysis of user content"),
					Schema:      UserAnalysisSchema,
					Strict:      openai.Bool(true),
				},
			},
		},
		Model:       a.model,
		Temperature: openai.Float(0.2),
		TopP:        openai.Float(0.4),
	})
	if err != nil {
		return fmt.Errorf("openai API error: %w", err)
	}

	// Check for empty response
	if len(resp.Choices) == 0 || len(resp.Choices[0].Message.Content) == 0 {
		return fmt.Errorf("%w: no response from model", ErrModelResponse)
	}

	// Parse response from AI
	var result *FlaggedUsers
	if err := sonic.Unmarshal([]byte(resp.Choices[0].Message.Content), &result); err != nil {
		return fmt.Errorf("JSON unmarshal error: %w", err)
	}

	// Validate AI responses
	a.validateAndUpdateFlaggedUsers(result, translatedInfos, originalInfos, reasonsMap, mu)

	return nil
}

// validateAndUpdateFlaggedUsers validates the flagged users and updates the flaggedUsers map.
func (a *UserAnalyzer) validateAndUpdateFlaggedUsers(
	result *FlaggedUsers, translatedInfos, originalInfos map[string]*types.User,
	reasonsMap map[uint64]types.Reasons[enum.UserReasonType], mu *sync.Mutex,
) {
	normalizer := utils.NewTextNormalizer()
	for _, flaggedUser := range result.Users {
		translatedInfo, exists := translatedInfos[flaggedUser.Name]
		originalInfo, hasOriginal := originalInfos[flaggedUser.Name]

		// Check if the flagged user exists in both maps
		if !exists && !hasOriginal {
			a.logger.Warn("AI flagged non-existent user", zap.String("username", flaggedUser.Name))
			continue
		}

		// Check if the user has social media links
		mu.Lock()
		originalInfo.HasSocials = flaggedUser.HasSocials
		mu.Unlock()

		// Skip results with no violations
		if flaggedUser.Reason == "" || flaggedUser.Reason == "NO_VIOLATIONS" {
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

		// Process flagged content to handle newlines
		processedContent := utils.SplitLines(flaggedUser.FlaggedContent)

		// Validate flagged content against user texts
		isValid := normalizer.ValidateWords(processedContent,
			translatedInfo.Name,
			translatedInfo.DisplayName,
			translatedInfo.Description)

		// If the flagged user is valid, update the reasons map
		if isValid {
			mu.Lock()
			if _, exists := reasonsMap[originalInfo.ID]; !exists {
				reasonsMap[originalInfo.ID] = make(types.Reasons[enum.UserReasonType])
			}

			reasonsMap[originalInfo.ID].Add(enum.UserReasonTypeDescription, &types.Reason{
				Message:    flaggedUser.Reason,
				Confidence: flaggedUser.Confidence,
				Evidence:   processedContent,
			})
			mu.Unlock()
		} else {
			a.logger.Warn("AI flagged content did not pass validation",
				zap.Uint64("userID", originalInfo.ID),
				zap.String("flaggedUsername", flaggedUser.Name),
				zap.String("username", originalInfo.Name),
				zap.String("description", originalInfo.Description),
				zap.Strings("flaggedContent", processedContent))
		}
	}
}

// prepareUserInfos translates user descriptions for different languages and encodings.
// Returns maps of both translated and original user infos for validation.
func (a *UserAnalyzer) prepareUserInfos(
	ctx context.Context, userInfos []*types.User,
) (map[string]*types.User, map[string]*types.User) {
	var (
		originalInfos   = make(map[string]*types.User)
		translatedInfos = make(map[string]*types.User)
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
			translated, err := utils.WithRetry(ctx, func() (string, error) {
				return a.translator.Translate(
					ctx,
					info.Description,
					"auto", // Auto-detect source language
					"en",   // Translate to English
				)
			}, utils.GetAIRetryOptions())
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

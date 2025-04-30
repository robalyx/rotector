package ai

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/bytedance/sonic"
	"github.com/openai/openai-go"
	"github.com/robalyx/rotector/internal/ai/client"
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
	UserSystemPrompt = `Instruction:
You MUST act as a Roblox content moderator specializing in detecting predatory behavior targeting minors.

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
      "reason": "Clear explanation specifying why the content is inappropriate",
      "flaggedContent": ["exact quote 1", "exact quote 2"],
      "confidence": 0.0-1.0,
      "hasSocials": true/false
    }
  ]
}

Key instructions:
1. You MUST return ALL users that either have violations OR contain social media links
2. When referring to users in the 'reason' field, use "the user" or "this account" instead of usernames
3. You MUST include exact quotes from the user's content in the 'flaggedContent' array when a violation is found
4. If no violations are found for a user, you MUST exclude from the response or set the 'reason' field to "NO_VIOLATIONS"
5. You MUST skip analysis for users with empty descriptions and without an inappropriate username/display name
6. You MUST set the 'hasSocials' field to true if the user's description contains any social media handles, links, or mentions
7. If a user has no violations but has social media links, you MUST only include the 'name' and 'hasSocials' fields for that user
8. You MUST ONLY flag users who exhibit predatory or inappropriate behavior
9. You MUST flag usernames and display names even if the description is empty as the name itself can be sufficient evidence

Confidence levels:
Assign the 'confidence' score based on the explicitness of the predatory indicators found, according to the following guidelines:
0.0: No inappropriate elements
0.1-0.3: Subtle inappropriate elements
0.4-0.6: Clear inappropriate content  
0.7-0.8: Strong inappropriate indicators
0.9-1.0: Explicit inappropriate content

Instruction:
Pay close attention to the following indicators of predatory behavior in descriptions:

1. Grooming Tactics:
- ANY attempt at building trust through friendly/caring language
- ANY attempt to establish private communication
- ANY attempt to move communication off-platform
- ANY use of manipulation or guilt tactics
- ANY promises of friendship/relationship
- ANY use of excessive compliments or flattery
- ANY creation of secrecy or exclusivity
- ANY attempt to isolate targets from others

2. Exploitation Indicators:
- ANY seeking of private interactions
- ANY offering or requesting of inappropriate content
- ANY inappropriate use of authority positions
- ANY targeting of specific age groups/genders
- ANY creation of power imbalances
- ANY attempt to normalize inappropriate behavior
- ANY use of coded language for inappropriate acts

3. Suspicious Communication Patterns:
- ANY coded language implying inappropriate activities
- ANY leading phrases implying secrecy
- ANY studio mentions or invites (ZERO EXCEPTIONS)
- ANY game or chat references that could enable private interactions
- ANY condo/con references
- ANY "exclusive" group invitations
- ANY private server invitations
- ANY age-restricted invitations
- ANY suspicious direct messaging demands
- ANY requests to "message first" or "dm first"
- ANY use of the spade symbol (♠) or similar symbols in suspicious contexts
- ANY use of slang with inappropriate context ("down", "dtf", etc.)
- ANY claims of following TOS/rules to avoid detection
- ANY roleplay requests or themes (ZERO EXCEPTIONS)
- ANY mentions of "trading" or variations which commonly refer to CSAM
- ANY mentions of "cheese" or "pizza" which are known code words for CSAM
- ANY use of "yk" or "you know" in suspicious contexts

4. Inappropriate Content:
- ANY sexual content or innuendo
- ANY sexual solicitation
- ANY erotic roleplay (ERP)
- ANY age-inappropriate dating content
- ANY non-consensual references
- ANY ownership/dominance references
- ANY adult community references
- ANY suggestive size references
- ANY inappropriate trading
- ANY degradation terms
- ANY breeding/heat themes
- ANY references to bulls or cuckolding content
- ANY raceplay or racial sexual stereotypes
- ANY fart/gas/smell references
- ANY fetish references

5. Technical Evasion:
- ANY bypassed inappropriate terms
- ANY Caesar cipher (ROT13 and other rotations)
- ANY deliberately misspelled inappropriate terms
- ANY references to "futa" or bypasses like "fmta", "fmt", etc.
- ANY references to "les" or similar LGBT+ terms used inappropriately
- ANY warnings or anti-predator messages (manipulation tactics)

7. Social Engineering:
- ANY friend requests with inappropriate context
- ANY terms of endearment used predatorily (mommy, daddy, kitten, etc.)
- ANY "special" or "exclusive" game pass offers
- ANY promises of rewards for buying passes
- ANY promises or offers of "fun"
- ANY references to "blue user" or "blue app"
- ANY directing to other profiles/accounts with a user identifier
- ANY use of innocent-sounding terms as code words
- ANY mentions of literacy or writing ability

Instruction: Pay close attention to usernames and display names that suggest predatory intentions, such as:
- ANY names exploiting authority or mentor roles
- ANY names suggesting sexual availability or soliciting inappropriate interactions 
- ANY names using pet names or diminutives suggestively (kitty, kitten, etc.)
- ANY names targeting minors or specific genders inappropriately
- ANY names using gender identity terms that could be used to target or groom (fem, femboy, femgirl, etc.)
- ANY names using ethnic or racial terms that could be used to target specific groups (latina, etc.)
- ANY names using coded language or suggestive terms related to inappropriate acts
- ANY names hinting at exploitation or predatory activities
- ANY references to adult content platforms or services
- ANY deliberately misspelled inappropriate terms
- ANY mentions of bull, fart, gas, smell, etc.

Instruction: You MUST flag ANY roleplay requests and themes because:
1. ANY roleplay can be used to groom or exploit children
2. ANY roleplay creates opportunities for predators to build trust
3. Even seemingly innocent roleplay can escalate to inappropriate content
4. There is no way to ensure roleplay remains appropriate

Instruction: When flagging inappropriate usernames or display names:
- Set the 'confidence' level based on how explicit or obvious the inappropriate content is
- Include the full username or display name as a single string in the 'flaggedContent' array
- Set the 'reason' field to clearly explain why the name is inappropriate and breakdown terms
- Consider combinations of words that together create inappropriate meanings

Instruction: You MUST consider ANY sexual content or references on Roblox as predatory behavior due to:
1. Roblox is primarily a children's platform
2. ANY sexual content in spaces meant for children is inherently predatory
3. ANY sexual usernames/content expose minors to inappropriate material
4. There is no legitimate reason for sexual content on a children's platform

IGNORE:
- Empty descriptions
- General social interactions
- Compliments on outfits/avatars
- Advertisements to join channels or tournaments
- Gender identity expression
- Bypass of appropriate terms
- Self-harm or suicide-related content
- Violence, gore, or disturbing content
- Sharing of personal information
- Random words or gibberish that are not ROT13`

	// UserRequestPrompt provides a reminder to follow system guidelines for user analysis.
	UserRequestPrompt = `Analyze these user profiles for predatory content and social media links.

Remember:
1. Return ALL users that either have violations OR contain social media links
2. Use "the user"/"this account" instead of usernames
3. Follow confidence level guide strictly
4. Always set hasSocials field accurately
5. For users with only social media links (no violations), include only name and hasSocials fields

Input:
`
)

var ErrBatchProcessing = errors.New("batch processing errors")

// UserSummary is a struct for user summaries for AI analysis.
type UserSummary struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName,omitempty"`
	Description string `json:"description"`
}

// FlaggedUsers holds a list of users that the AI has identified as inappropriate.
// The JSON schema is used to ensure consistent responses from the AI.
type FlaggedUsers struct {
	Users []FlaggedUser `json:"users" jsonschema_description:"List of users that have been flagged for inappropriate content"`
}

// FlaggedUser contains the AI's analysis results for a single user.
// The confidence score and flagged content help moderators make decisions.
type FlaggedUser struct {
	Name           string   `json:"name"           jsonschema_description:"Username of the flagged account"`
	Reason         string   `json:"reason"         jsonschema_description:"Clear explanation of why the user was flagged"`
	FlaggedContent []string `json:"flaggedContent" jsonschema_description:"List of exact quotes from the user's content that were flagged"`
	Confidence     float64  `json:"confidence"     jsonschema_description:"Overall confidence score for the violations (0.0-1.0)"`
	HasSocials     bool     `json:"hasSocials"     jsonschema_description:"Whether the user's description contains social media"`
}

// UserAnalyzer handles AI-based content analysis using OpenAI models.
type UserAnalyzer struct {
	chat        client.ChatCompletions
	minify      *minify.M
	translator  *translator.Translator
	analysisSem *semaphore.Weighted
	logger      *zap.Logger
	model       string
	batchSize   int
}

// UserAnalysisSchema is the JSON schema for the user analysis response.
var UserAnalysisSchema = utils.GenerateSchema[FlaggedUsers]()

// NewUserAnalyzer creates an UserAnalyzer with separate models for user and friend analysis.
func NewUserAnalyzer(app *setup.App, translator *translator.Translator, logger *zap.Logger) *UserAnalyzer {
	// Create a minifier for JSON optimization
	m := minify.New()
	m.AddFunc(ApplicationJSON, json.Minify)

	return &UserAnalyzer{
		chat:        app.AIClient.Chat(),
		minify:      m,
		translator:  translator,
		analysisSem: semaphore.NewWeighted(int64(app.Config.Worker.BatchSizes.UserAnalysis)),
		logger:      logger.Named("ai_user"),
		model:       app.Config.Common.OpenAI.DefaultModel,
		batchSize:   app.Config.Worker.BatchSizes.UserAnalysisBatch,
	}
}

// ProcessUsers analyzes user content for a batch of users.
func (a *UserAnalyzer) ProcessUsers(users []*types.User, reasonsMap map[uint64]types.Reasons[enum.UserReasonType]) {
	// Track counts before processing
	existingFlags := len(reasonsMap)
	numBatches := (len(users) + a.batchSize - 1) / a.batchSize

	// Process batches concurrently
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	var (
		p  = pool.New().WithContext(ctx)
		mu sync.Mutex
	)

	for i := range numBatches {
		start := i * a.batchSize
		end := min(start+a.batchSize, len(users))

		infoBatch := users[start:end]
		p.Go(func(ctx context.Context) error {
			// Acquire semaphore
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
		return
	}

	a.logger.Info("Received AI user analysis",
		zap.Int("totalUsers", len(users)),
		zap.Int("newFlags", len(reasonsMap)-existingFlags))
}

// processUserBatch handles the AI analysis for a batch of user summaries.
func (a *UserAnalyzer) processUserBatch(ctx context.Context, batch []UserSummary) (*FlaggedUsers, error) {
	// Convert to JSON
	userInfoJSON, err := sonic.Marshal(batch)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrJSONProcessing, err)
	}

	// Minify JSON to reduce token usage
	userInfoJSON, err = a.minify.Bytes(ApplicationJSON, userInfoJSON)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrJSONProcessing, err)
	}

	// Prepare request prompt with user info
	requestPrompt := UserRequestPrompt + string(userInfoJSON)

	// Prepare request parameters
	params := openai.ChatCompletionNewParams{
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
		Temperature: openai.Float(0.1),
		TopP:        openai.Float(0.2),
	}

	params = client.WithReasoning(params, client.ReasoningOptions{
		Effort:    openai.ReasoningEffortHigh,
		MaxTokens: 8192,
		Exclude:   false,
	})

	// Make API request with retry
	return utils.WithRetry(ctx, func() (*FlaggedUsers, error) {
		resp, err := a.chat.New(ctx, params)
		if err != nil {
			return nil, fmt.Errorf("openai API error: %w", err)
		}

		// Check for empty response
		if len(resp.Choices) == 0 || len(resp.Choices[0].Message.Content) == 0 {
			return nil, fmt.Errorf("%w: no response from model", ErrModelResponse)
		}

		// Extract thought process
		message := resp.Choices[0].Message
		if thought := message.JSON.ExtraFields["reasoning"]; thought.IsPresent() {
			a.logger.Debug("AI user analysis thought process",
				zap.String("model", resp.Model),
				zap.String("thought", thought.Raw()))
		}

		// Parse response from AI
		var result *FlaggedUsers
		if err := sonic.Unmarshal([]byte(message.Content), &result); err != nil {
			return nil, fmt.Errorf("JSON unmarshal error: %w", err)
		}

		return result, nil
	}, utils.GetAIRetryOptions())
}

// processBatch handles analysis for a batch of users.
func (a *UserAnalyzer) processBatch(
	ctx context.Context, userInfos []*types.User, reasonsMap map[uint64]types.Reasons[enum.UserReasonType], mu *sync.Mutex,
) error {
	// Translate all descriptions concurrently
	translatedInfos, originalInfos := a.prepareUserInfos(ctx, userInfos)

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

	// Create operation function for batch processing
	minBatchSize := max(len(userInfosWithoutID)/4, 1)
	result, err := utils.WithRetrySplitBatch(
		ctx, userInfosWithoutID, len(userInfosWithoutID), minBatchSize,
		func(batch []UserSummary) (*FlaggedUsers, error) {
			return a.processUserBatch(ctx, batch)
		},
		utils.GetAIRetryOptions(), a.logger,
	)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrModelResponse, err)
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

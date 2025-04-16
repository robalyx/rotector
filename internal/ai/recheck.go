package ai

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/bytedance/sonic"
	"github.com/openai/openai-go"
	"github.com/robalyx/rotector/internal/ai/client"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/database/types/enum"
	"github.com/robalyx/rotector/internal/setup"
	"github.com/robalyx/rotector/pkg/utils"
	"github.com/sourcegraph/conc/pool"
	"github.com/tdewolff/minify/v2"
	"github.com/tdewolff/minify/v2/json"
	"go.uber.org/zap"
	"golang.org/x/sync/semaphore"
)

const (
	RecheckSystemPrompt = `You are a senior Roblox content moderator specializing in reviewing flagged content.
Your task is to carefully review users who were flagged by another AI system.
Provide constructive feedback about potential mistakes or oversights in the flagging process.

Input format:
{
  "users": [
    {
      "username": "string",
      "displayName": "string",
      "description": "string",
      "originalReason": "string"
    }
  ]
}

Output format:
{
  "users": [
    {
      "username": "string",
      "analysis": [
        "Point 1 about why the flag might be incorrect",
        "Point 2 about alternative interpretations",
        "Point 3 about context or intent"
      ],
      "recommendation": "Clear statement about whether this deserves reconsideration"
    }
  ]
}

Key instructions:
1. You MUST carefully analyze each user's profile content
2. You MUST consider BOTH predatory behavior AND general content guidelines
3. You MUST provide feedback about any concerns with the flagging
4. DO NOT unflag just because content isn't explicit enough
5. If you are unsure about the flag, you MUST explain it in your feedback
6. If ALL users were correctly flagged, return an empty JSON response: {"users": []}
7. DO NOT repeat or quote any inappropriate content in your analysis
8. DO NOT use sexually explicit or inappropriate language in your feedback
9. Keep your feedback professional and clinical in tone

REMEMBER: Roblox is a platform primarily used by children. Content should be kept strictly appropriate for all ages.

These guidelines were used by the other AI system to flag inappropriate content.

` + SharedUserContentGuidelines

	RecheckRequestPrompt = `Please review these flagged users.
Provide constructive feedback about potential mistakes in the flagging process.

Remember:
1. Explain why you think the flag might be incorrect
2. DO NOT unflag content with suggestive or implicit meanings
3. Maintain a balanced perspective and err on the side of caution when children's safety is concerned.

IMPORTANT: If ALL users were correctly flagged, return an empty JSON response: {"users": []}

Input:
`

	VerificationSystemPrompt = `You are a Roblox content moderator working with a partner to verify flagged content.
Your task is to make the final decision about whether users were incorrectly flagged, taking into account your partner's feedback.

Partner's feedback format:
{
  "users": [
    {
      "username": "string",
      "analysis": [
        "Point 1 about why the flag might be incorrect",
        "Point 2 with specific examples",
        "Point 3 considering alternative interpretations"
      ],
      "recommendation": "Clear statement about whether this deserves reconsideration"
    }
  ]
}

Input format:
{
  "users": [
    {
      "username": "string",
      "displayName": "string",
      "description": "string",
      "originalReason": "string"
    }
  ]
}

Output format:
{
  "users": [
    {
      "username": "string",
      "incorrectlyFlagged": boolean,
      "explanation": "string (required if incorrectlyFlagged is true)"
    }
  ]
}

Key instructions:
1. You MUST carefully consider your partner's feedback about each user
2. You MUST remember that your partner can make mistakes
3. DO NOT unflag if the content violates ANY guideline, no matter how minor
4. DO NOT unflag just because content isn't explicit enough
5. DO NOT unflag content with suggestive or implicit meanings
6. DO NOT unflag if there are ANY possible inappropriate interpretations
7. DO NOT unflag just because there might be innocent interpretations
8. DO NOT unflag based on user claims or promises to change
9. You MUST provide a short explanation for your decisions

IMPORTANT: A user should ONLY be marked as incorrectly flagged if their content is COMPLETELY CLEAN and violates ZERO guidelines.
If their content violates ANY guideline, even slightly or implicitly, they should remain flagged.

Remember: Roblox is a platform primarily used by children. Content should be kept strictly appropriate for all ages.

These guidelines below are used to flag inappropriate content.

` + SharedUserContentGuidelines

	VerificationRequestPrompt = `Please review these flagged users.
Determine if any were incorrectly flagged, taking into account the feedback from your partner.

Partner's feedback:
%s

Remember:
1. Your partner can make mistakes so use your own judgment
2. DO NOT unflag if there are ANY possible inappropriate interpretations
3. DO NOT unflag just because there might be innocent interpretations
4. DO NOT unflag based on user claims or promises
5. Provide a short explanation for any users you determine were incorrectly flagged

CRITICAL: Only mark a user as incorrectly flagged if their content is COMPLETELY CLEAN with NO possible inappropriate interpretations.
The existence of a likely possible innocent meaning does NOT make the content clean.

Input:
`
)

// RecheckAnalyzer handles verification of flagged users through a two-stage process.
type RecheckAnalyzer struct {
	chat         client.ChatCompletions
	minify       *minify.M
	analysisSem  *semaphore.Weighted
	logger       *zap.Logger
	defaultModel string
	recheckModel string
	batchSize    int
}

// VerificationResult contains the final verification of a flagged user.
type VerificationResult struct {
	Username           string `json:"username"           jsonschema_description:"Username of the account being analyzed"`
	IncorrectlyFlagged bool   `json:"incorrectlyFlagged" jsonschema_description:"Whether the user was incorrectly flagged"`
	Explanation        string `json:"explanation"        jsonschema_description:"Explanation of why the flag was incorrect (if applicable)"`
}

// VerificationResults contains a list of verification results.
type VerificationResults struct {
	Users []VerificationResult `json:"users" jsonschema_description:"List of verification results for flagged users"`
}

// UserInput contains the user information and original reason for recheck analysis.
type UserInput struct {
	Username       string `json:"username"       jsonschema_description:"Username of the account being analyzed"`
	DisplayName    string `json:"displayName"    jsonschema_description:"Display name of the account"`
	Description    string `json:"description"    jsonschema_description:"Profile description of the account"`
	OriginalReason string `json:"originalReason" jsonschema_description:"Original reason for flagging the account"`
}

// VerificationResultsSchema is the JSON schema for the verification results.
var VerificationResultsSchema = utils.GenerateSchema[VerificationResults]()

// NewRecheckAnalyzer creates a new RecheckAnalyzer instance.
func NewRecheckAnalyzer(app *setup.App, logger *zap.Logger) *RecheckAnalyzer {
	// Create a minifier for JSON optimization
	m := minify.New()
	m.AddFunc(ApplicationJSON, json.Minify)

	return &RecheckAnalyzer{
		chat:         app.AIClient.Chat(),
		minify:       m,
		analysisSem:  semaphore.NewWeighted(int64(app.Config.Worker.BatchSizes.RecheckAnalysis)),
		logger:       logger.Named("ai_recheck"),
		defaultModel: app.Config.Common.OpenAI.Model,
		recheckModel: app.Config.Common.OpenAI.RecheckModel,
		batchSize:    app.Config.Worker.BatchSizes.RecheckAnalysisBatch,
	}
}

// ProcessUsers analyzes users who were flagged to verify the flags.
func (a *RecheckAnalyzer) ProcessUsers(users []*types.User, reasonsMap map[uint64]types.Reasons[enum.UserReasonType]) {
	// Track counts before processing
	existingFlags := len(reasonsMap)

	// Filter users that need rechecking
	validUsers := make([]*types.User, 0, len(users))
	for _, user := range users {
		reasons, exists := reasonsMap[user.ID]
		if !exists {
			continue
		}

		// Skip if user has more than one reason type
		if len(reasons) != 1 {
			continue
		}

		// Skip if the only reason is not from user description
		if _, hasDescription := reasons[enum.UserReasonTypeDescription]; !hasDescription {
			continue
		}

		validUsers = append(validUsers, user)
	}

	// Skip if no valid users to process
	if len(validUsers) == 0 {
		return
	}

	numBatches := (len(validUsers) + a.batchSize - 1) / a.batchSize

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	var (
		p  = pool.New().WithContext(ctx)
		mu sync.Mutex
	)

	for i := range numBatches {
		start := i * a.batchSize
		end := min(start+a.batchSize, len(validUsers))

		userBatch := validUsers[start:end]
		p.Go(func(ctx context.Context) error {
			// Acquire semaphore before making AI request
			if err := a.analysisSem.Acquire(ctx, 1); err != nil {
				return fmt.Errorf("failed to acquire semaphore: %w", err)
			}
			defer a.analysisSem.Release(1)

			// Process batch
			if err := a.processBatch(ctx, userBatch, reasonsMap, &mu); err != nil {
				a.logger.Error("Failed to process batch",
					zap.Error(err),
					zap.Int("batchStart", start),
					zap.Int("batchEnd", end))
				return err
			}
			return nil
		})
	}

	if err := p.Wait(); err != nil {
		a.logger.Error("Error during user recheck", zap.Error(err))
		return
	}

	a.logger.Info("Finished rechecking users",
		zap.Int("totalUsers", len(users)),
		zap.Int("recheckedUsers", len(validUsers)),
		zap.Int("deletedFlags", existingFlags-len(reasonsMap)))
}

// processBatch handles the recheck analysis for a batch of users.
func (a *RecheckAnalyzer) processBatch(
	ctx context.Context, userBatch []*types.User, reasonsMap map[uint64]types.Reasons[enum.UserReasonType], mu *sync.Mutex,
) error {
	// Prepare inputs for the batch
	inputs := make([]UserInput, len(userBatch))
	for i, user := range userBatch {
		reason := reasonsMap[user.ID][enum.UserReasonTypeDescription]
		inputs[i] = UserInput{
			Username:       user.Name,
			DisplayName:    user.DisplayName,
			Description:    user.Description,
			OriginalReason: reason.Message,
		}
	}

	// Convert to JSON
	inputJSON, err := sonic.Marshal(inputs)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrJSONProcessing, err)
	}

	// Minify JSON to reduce token usage
	inputJSON, err = a.minify.Bytes(ApplicationJSON, inputJSON)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrJSONProcessing, err)
	}

	// Get feedback from recheck model
	feedback, err := a.getRecheckFeedback(ctx, string(inputJSON))
	if err != nil {
		return fmt.Errorf("failed to get recheck feedback: %w", err)
	}

	// Get final verification using feedback
	results, err := a.getVerification(ctx, string(inputJSON), feedback)
	if err != nil {
		return fmt.Errorf("failed to get verification: %w", err)
	}

	// Process verification results
	resultsMap := make(map[string]VerificationResult)
	for _, result := range results.Users {
		resultsMap[result.Username] = result
	}

	for _, user := range userBatch {
		if result, exists := resultsMap[user.Name]; exists && result.IncorrectlyFlagged {
			mu.Lock()
			originalReasons := reasonsMap[user.ID]
			delete(reasonsMap, user.ID)
			mu.Unlock()

			a.logger.Info("Removed incorrectly flagged user",
				zap.Uint64("userID", user.ID),
				zap.String("username", user.Name),
				zap.Any("originalReasons", originalReasons),
				zap.String("explanation", result.Explanation))
		}
	}

	return nil
}

// getRecheckFeedback gets constructive feedback about flagged users from the recheck model.
func (a *RecheckAnalyzer) getRecheckFeedback(ctx context.Context, inputJSON string) (string, error) {
	resp, err := a.chat.New(ctx, openai.ChatCompletionNewParams{
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(RecheckSystemPrompt),
			openai.UserMessage(RecheckRequestPrompt + inputJSON),
		},
		Model:       a.recheckModel,
		Temperature: openai.Float(0.4),
		TopP:        openai.Float(0.2),
	})
	if err != nil {
		return "", fmt.Errorf("openai API error: %w", err)
	}

	// Check for empty response
	if len(resp.Choices) == 0 || len(resp.Choices[0].Message.Content) == 0 {
		return "", fmt.Errorf("%w: no response from recheck model", ErrModelResponse)
	}

	// Extract thought process and clean text
	thought, cleanText := utils.ExtractThoughtProcess(resp.Choices[0].Message.Content)

	a.logger.Debug("Recheck feedback",
		zap.String("model", resp.Model),
		zap.String("thought", thought),
		zap.String("content", cleanText))

	return cleanText, nil
}

// getVerification gets final verification using the recheck feedback.
func (a *RecheckAnalyzer) getVerification(ctx context.Context, inputJSON, feedback string) (*VerificationResults, error) {
	resp, err := a.chat.New(ctx, openai.ChatCompletionNewParams{
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(VerificationSystemPrompt),
			openai.UserMessage(fmt.Sprintf(VerificationRequestPrompt, feedback) + inputJSON),
		},
		ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONSchema: &openai.ResponseFormatJSONSchemaParam{
				JSONSchema: openai.ResponseFormatJSONSchemaJSONSchemaParam{
					Name:        "verificationResults",
					Description: openai.String("Final verification of flagged users"),
					Schema:      VerificationResultsSchema,
					Strict:      openai.Bool(true),
				},
			},
		},
		Model:       a.defaultModel,
		Temperature: openai.Float(0.2),
		TopP:        openai.Float(0.1),
	})
	if err != nil {
		return nil, fmt.Errorf("openai API error: %w", err)
	}

	// Check for empty response
	if len(resp.Choices) == 0 || len(resp.Choices[0].Message.Content) == 0 {
		return nil, fmt.Errorf("%w: no response from verification model", ErrModelResponse)
	}

	// Extract thought process and clean JSON response
	thought, cleanJSON := utils.ExtractThoughtProcess(resp.Choices[0].Message.Content)
	if thought != "" {
		a.logger.Debug("AI verification thought process",
			zap.String("thought", thought),
			zap.String("model", resp.Model))
	}

	// Parse response
	var results VerificationResults
	if err := sonic.Unmarshal([]byte(cleanJSON), &results); err != nil {
		return nil, fmt.Errorf("JSON unmarshal error: %w", err)
	}

	return &results, nil
}

package ai

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/bytedance/sonic"
	"github.com/openai/openai-go"
	"github.com/robalyx/rotector/internal/ai/client"
	"github.com/robalyx/rotector/internal/database"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/database/types/enum"
	"github.com/robalyx/rotector/internal/setup"
	"github.com/robalyx/rotector/pkg/utils"
	"github.com/tdewolff/minify/v2"
	"github.com/tdewolff/minify/v2/json"
	"go.uber.org/zap"
	"golang.org/x/sync/semaphore"
)

const (
	// IvanSystemPrompt provides instructions for analyzing user chat messages.
	IvanSystemPrompt = `You are an AI moderator analyzing chat messages from "Write A Letter".
It is a Roblox game where players write letters and notes to friends or strangers.
This game is intended for innocent letter writing and socializing.
However, it is frequently misused for predatory behavior and inappropriate sexual content.

Input format:
{
  "userId": 123456789,
  "username": "username",
  "messages": [
    {
      "dateTime": "2024-01-01T12:00:00Z",
      "message": "message content"
    }
  ]
}

Output format:
{
  "isInappropriate": true/false,
  "reason": "Clear explanation in one sentence",
  "evidence": ["worst message 1", "worst message 2", ...],
  "confidence": 0.0-1.0
}

Key rules:
1. Focus on detecting predatory behavior and sexual content
2. Return at most 25 of the worst messages as evidence if inappropriate
3. Include full message content in evidence
4. Set confidence based on severity and pattern of behavior
5. Only flag users who are predators, not potential victims
6. Consider message patterns and context over time

Confidence levels:
0.0: No inappropriate content
0.1-0.3: Subtle predatory elements
0.4-0.6: Clear inappropriate content
0.7-0.8: Strong predatory indicators
0.9-1.0: Explicit predatory behavior

Look for:
- Sexual content or innuendos
- Grooming behavior
- Attempts to move conversations private
- Inappropriate requests or demands
- References to adult content
- Targeting of minors
- Coded language for inappropriate activities
- Pattern of predatory behavior
- Sexual harassment
- Explicit content sharing
- Erotic roleplay (ERP) attempts
- Attempts to establish inappropriate relationships
- Requests for inappropriate photos or content

Ignore:
- General profanity
- Non-sexual harassment
- Spam messages
- Normal game discussions
- Friend requests without inappropriate context
- Non-sexual roleplay
- General conversation
- Internet slang and memes
- Harmless Gen Z humor
- Normal socializing
- Platonic expressions of friendship
- General compliments
- Game-related discussions`

	// IvanRequestPrompt provides the template for message analysis requests.
	IvanRequestPrompt = `Analyze these chat messages for inappropriate content.

Remember:
- Only flag users showing predatory or inappropriate sexual behavior
- Include at most 25 of the worst messages as evidence if inappropriate
- Consider message patterns and context
- Follow confidence level guide strictly

Messages to analyze:
`
)

// MessageForAI represents a message to be analyzed by the AI.
type MessageForAI struct {
	DateTime time.Time `json:"dateTime"`
	Message  string    `json:"message"`
}

// IvanAnalysisResponse represents the AI's analysis of a user's messages.
type IvanAnalysisResponse struct {
	IsInappropriate bool     `json:"isInappropriate"`
	Reason          string   `json:"reason"`
	Evidence        []string `json:"evidence"`
	Confidence      float64  `json:"confidence"`
}

// IvanAnalyzer handles AI-based analysis of user chat messages.
type IvanAnalyzer struct {
	db          database.Client
	chat        client.ChatCompletions
	minify      *minify.M
	analysisSem *semaphore.Weighted
	logger      *zap.Logger
	model       string
	batchSize   int
}

// IvanAnalysisSchema is the JSON schema for the message analysis response.
var IvanAnalysisSchema = utils.GenerateSchema[IvanAnalysisResponse]()

// NewIvanAnalyzer creates a new IvanAnalyzer.
func NewIvanAnalyzer(app *setup.App, logger *zap.Logger) *IvanAnalyzer {
	m := minify.New()
	m.AddFunc("application/json", json.Minify)

	return &IvanAnalyzer{
		db:          app.DB,
		chat:        app.AIClient.Chat(),
		minify:      m,
		analysisSem: semaphore.NewWeighted(int64(app.Config.Worker.BatchSizes.MessageAnalysis)),
		logger:      logger.Named("ai_ivan"),
		model:       app.Config.Common.OpenAI.Model,
		batchSize:   app.Config.Worker.BatchSizes.MessageAnalysisBatch,
	}
}

// ProcessUsers analyzes multiple users' chat messages for inappropriate content.
func (a *IvanAnalyzer) ProcessUsers(users []*types.User, reasonsMap map[uint64]types.Reasons[enum.UserReasonType]) {
	if len(users) == 0 {
		return
	}

	// Extract user IDs
	userIDs := make([]uint64, len(users))
	userMap := make(map[uint64]*types.User)
	for i, user := range users {
		userIDs[i] = user.ID
		userMap[user.ID] = user
	}

	// Get all user messages and mark them as checked
	messages, err := a.db.Model().Ivan().GetAndMarkUsersMessages(context.Background(), userIDs)
	if err != nil {
		a.logger.Error("Failed to get ivan messages",
			zap.Error(err),
			zap.Int("userCount", len(users)))
		return
	}

	// Track counts before processing
	existingFlags := len(reasonsMap)

	var mu sync.Mutex
	for userID, userMsgs := range messages {
		user := userMap[userID]
		if err := a.processMessages(context.Background(), user.ID, user.Name, userMsgs, reasonsMap, &mu); err != nil {
			a.logger.Error("Failed to process ivan messages",
				zap.Error(err),
				zap.Uint64("userID", user.ID))
		}
	}

	a.logger.Info("Processed ivan messages",
		zap.Int("totalUsers", len(users)),
		zap.Int("newFlags", len(reasonsMap)-existingFlags))
}

// processUserMessages filters and deduplicates messages for a user.
func (a *IvanAnalyzer) processUserMessages(messages []*types.IvanMessage) []MessageForAI {
	if len(messages) == 0 {
		return nil
	}

	// Initialize normalizer for text normalization
	normalizer := utils.NewTextNormalizer()

	// Use map to track unique normalized messages
	seen := make(map[string]struct{})
	uniqueMessages := make([]MessageForAI, 0, len(messages))

	for _, msg := range messages {
		// Skip messages that are too long (likely spam)
		if len(msg.Message) > 500 {
			continue
		}

		// Normalize message text
		normalized := normalizer.Normalize(msg.Message)
		if normalized == "" {
			continue
		}

		// Skip if we've seen a similar message
		if _, exists := seen[normalized]; exists {
			continue
		}

		// Add to unique messages
		seen[normalized] = struct{}{}
		uniqueMessages = append(uniqueMessages, MessageForAI{
			DateTime: msg.DateTime,
			Message:  msg.Message,
		})
	}

	return uniqueMessages
}

// processMessages analyzes a user's chat messages for inappropriate content.
func (a *IvanAnalyzer) processMessages(
	ctx context.Context, userID uint64, username string, messages []*types.IvanMessage,
	reasonsMap map[uint64]types.Reasons[enum.UserReasonType], mu *sync.Mutex,
) error {
	// Skip if no messages
	if len(messages) == 0 {
		return nil
	}

	// Process and deduplicate messages
	uniqueMessages := a.processUserMessages(messages)
	if len(uniqueMessages) == 0 {
		return nil
	}

	// Prepare request data
	type RequestData struct {
		UserID   uint64         `json:"userId"`
		Username string         `json:"username"`
		Messages []MessageForAI `json:"messages"`
	}

	request := RequestData{
		UserID:   userID,
		Username: username,
		Messages: uniqueMessages,
	}

	// Convert to JSON
	requestJSON, err := sonic.Marshal(request)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	// Minify JSON
	minifiedJSON, err := a.minify.Bytes("application/json", requestJSON)
	if err != nil {
		return fmt.Errorf("failed to minify JSON: %w", err)
	}

	// Acquire semaphore
	if err := a.analysisSem.Acquire(ctx, 1); err != nil {
		return fmt.Errorf("failed to acquire semaphore: %w", err)
	}
	defer a.analysisSem.Release(1)

	// Generate analysis with retry
	result, err := utils.WithRetry(ctx, func() (*IvanAnalysisResponse, error) {
		resp, err := a.chat.New(ctx, openai.ChatCompletionNewParams{
			Messages: []openai.ChatCompletionMessageParamUnion{
				openai.SystemMessage(IvanSystemPrompt),
				openai.UserMessage(IvanRequestPrompt + string(minifiedJSON)),
			},
			ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
				OfJSONSchema: &openai.ResponseFormatJSONSchemaParam{
					JSONSchema: openai.ResponseFormatJSONSchemaJSONSchemaParam{
						Name:        "ivanAnalysis",
						Description: openai.String("Analysis of user chat messages"),
						Schema:      IvanAnalysisSchema,
						Strict:      openai.Bool(true),
					},
				},
			},
			Model:       a.model,
			Temperature: openai.Float(0.2),
			TopP:        openai.Float(0.4),
		})
		if err != nil {
			return nil, fmt.Errorf("openai API error: %w", err)
		}

		// Check for empty response
		if len(resp.Choices) == 0 || len(resp.Choices[0].Message.Content) == 0 {
			return nil, fmt.Errorf("%w: no response from model", ErrModelResponse)
		}

		// Parse response
		var result IvanAnalysisResponse
		if err := sonic.Unmarshal([]byte(resp.Choices[0].Message.Content), &result); err != nil {
			return nil, fmt.Errorf("JSON unmarshal error: %w", err)
		}

		return &result, nil
	}, utils.GetAIRetryOptions())
	if err != nil {
		return fmt.Errorf("%w: %w", ErrModelResponse, err)
	}

	// Skip if not inappropriate
	if !result.IsInappropriate {
		return nil
	}

	// Ensure max 25 evidence messages
	evidence := result.Evidence
	if len(evidence) > 25 {
		evidence = evidence[:25]
	}

	// Validate evidence
	normalizer := utils.NewTextNormalizer()
	allMessages := make([]string, len(messages))
	for i, msg := range messages {
		allMessages[i] = msg.Message
	}

	isValid := normalizer.ValidateWords(evidence, allMessages...)
	if !isValid {
		a.logger.Warn("AI flagged content did not pass validation",
			zap.Uint64("userID", userID),
			zap.String("username", username),
			zap.Strings("evidence", evidence))
		return nil
	}

	// Update the reasons map
	mu.Lock()
	if _, exists := reasonsMap[userID]; !exists {
		reasonsMap[userID] = make(types.Reasons[enum.UserReasonType])
	}
	reasonsMap[userID].Add(enum.UserReasonTypeChat, &types.Reason{
		Message:    "Flagged in WAL (thx ivannetta) - " + result.Reason,
		Confidence: result.Confidence,
		Evidence:   evidence,
	})
	mu.Unlock()

	return nil
}

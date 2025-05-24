package ai

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
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

// MessageForAI represents a message to be analyzed by the AI.
type MessageForAI struct {
	DateTime time.Time `json:"dateTime" jsonschema_description:"Timestamp when the message was sent"`
	Message  string    `json:"message"  jsonschema_description:"Content of the message"`
}

// IvanRequest represents the request data for the AI.
type IvanRequest struct {
	UserID   uint64         `json:"userId"   jsonschema_description:"User ID of the account being analyzed"`
	Username string         `json:"username" jsonschema_description:"Username of the account being analyzed"`
	Messages []MessageForAI `json:"messages" jsonschema_description:"List of messages to analyze"`
}

// IvanAnalysisResponse represents the AI's analysis of a user's messages.
type IvanAnalysisResponse struct {
	IsInappropriate bool     `json:"isInappropriate" jsonschema_description:"Whether the user's messages are inappropriate"`
	Reason          string   `json:"reason"          jsonschema_description:"Explanation of why the messages are inappropriate"`
	Evidence        []string `json:"evidence"        jsonschema_description:"List of specific messages that were flagged"`
	Confidence      float64  `json:"confidence"      jsonschema_description:"Confidence score for the analysis (0.0-1.0)"`
}

// IvanAnalyzer handles AI-based analysis of user chat messages.
type IvanAnalyzer struct {
	db          database.Client
	chat        client.ChatCompletions
	minify      *minify.M
	analysisSem *semaphore.Weighted
	logger      *zap.Logger
	textLogger  *zap.Logger
	textDir     string
	model       string
	batchSize   int
}

// IvanAnalysisSchema is the JSON schema for the message analysis response.
var IvanAnalysisSchema = utils.GenerateSchema[IvanAnalysisResponse]()

// NewIvanAnalyzer creates a new IvanAnalyzer.
func NewIvanAnalyzer(app *setup.App, logger *zap.Logger) *IvanAnalyzer {
	m := minify.New()
	m.AddFunc("application/json", json.Minify)

	// Get text logger
	textLogger, textDir, err := app.LogManager.GetTextLogger("ivan_analyzer")
	if err != nil {
		logger.Error("Failed to create text logger", zap.Error(err))
		textLogger = logger
	}

	return &IvanAnalyzer{
		db:          app.DB,
		chat:        app.AIClient.Chat(),
		minify:      m,
		analysisSem: semaphore.NewWeighted(int64(app.Config.Worker.BatchSizes.MessageAnalysis)),
		logger:      logger.Named("ai_ivan"),
		textLogger:  textLogger,
		textDir:     textDir,
		model:       app.Config.Common.OpenAI.IvanModel,
		batchSize:   app.Config.Worker.BatchSizes.MessageAnalysisBatch,
	}
}

// ProcessUsers analyzes multiple users' chat messages for inappropriate content.
func (a *IvanAnalyzer) ProcessUsers(
	ctx context.Context, users []*types.ReviewUser, reasonsMap map[uint64]types.Reasons[enum.UserReasonType],
) {
	if len(users) == 0 {
		return
	}

	// Extract user IDs
	userIDs := make([]uint64, len(users))
	userMap := make(map[uint64]*types.ReviewUser)
	for i, user := range users {
		userIDs[i] = user.ID
		userMap[user.ID] = user
	}

	// Get all user messages and mark them as checked
	messages, err := a.db.Model().Ivan().GetAndMarkUsersMessages(ctx, userIDs)
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
		if err := a.processMessages(ctx, user.ID, user.Name, userMsgs, reasonsMap, &mu); err != nil {
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

// processIvanBatch handles the AI analysis for a batch of messages.
func (a *IvanAnalyzer) processIvanBatch(ctx context.Context, batchRequest IvanRequest) (*IvanAnalysisResponse, error) {
	// Convert to JSON
	requestJSON, err := sonic.Marshal(batchRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Minify JSON
	minifiedJSON, err := a.minify.Bytes("application/json", requestJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to minify JSON: %w", err)
	}

	// Prepare chat completion parameters
	params := openai.ChatCompletionNewParams{
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
	}

	// Make API request
	var result IvanAnalysisResponse
	err = a.chat.NewWithRetry(ctx, params, func(resp *openai.ChatCompletion, err error) error {
		// Handle API error
		if err != nil {
			return fmt.Errorf("openai API error: %w", err)
		}

		// Check for empty response
		if len(resp.Choices) == 0 || len(resp.Choices[0].Message.Content) == 0 {
			return fmt.Errorf("%w: no response from model", utils.ErrModelResponse)
		}

		// Extract thought process
		message := resp.Choices[0].Message
		if thought := message.JSON.ExtraFields["reasoning"]; thought.Valid() {
			a.logger.Debug("AI ivan analysis thought process",
				zap.String("model", resp.Model),
				zap.String("thought", thought.Raw()))
		}

		// Parse response
		if err := sonic.Unmarshal([]byte(message.Content), &result); err != nil {
			return fmt.Errorf("JSON unmarshal error: %w", err)
		}

		return nil
	})

	return &result, err
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

	// Acquire semaphore
	if err := a.analysisSem.Acquire(ctx, 1); err != nil {
		return fmt.Errorf("failed to acquire semaphore: %w", err)
	}
	defer a.analysisSem.Release(1)

	// Handle content blocking
	minBatchSize := max(len(uniqueMessages)/4, 1)

	var result *IvanAnalysisResponse
	err := utils.WithRetrySplitBatch(
		ctx, uniqueMessages, len(uniqueMessages), minBatchSize, utils.GetAIRetryOptions(),
		func(batch []MessageForAI) error {
			var err error
			result, err = a.processIvanBatch(ctx, IvanRequest{
				UserID:   userID,
				Username: username,
				Messages: batch,
			})
			return err
		},
		func(batch []MessageForAI) {
			// Log detailed content to text logger
			a.textLogger.Warn("Content blocked in ivan analysis batch",
				zap.Uint64("user_id", userID),
				zap.String("username", username),
				zap.Int("batch_size", len(batch)),
				zap.Any("messages", batch))

			// Save blocked messages to file
			filename := fmt.Sprintf("%d_%s_%s.txt", userID, username, time.Now().Format("20060102_150405"))
			filepath := filepath.Join(a.textDir, filename)

			var buf bytes.Buffer
			for _, msg := range batch {
				buf.WriteString(fmt.Sprintf("Time: %s\nMessage: %s\n\n",
					msg.DateTime.Format(time.RFC3339), msg.Message))
			}

			if err := os.WriteFile(filepath, buf.Bytes(), 0o600); err != nil {
				a.textLogger.Error("Failed to save blocked messages",
					zap.Error(err),
					zap.String("path", filepath))
				return
			}

			a.textLogger.Info("Saved blocked messages",
				zap.String("path", filepath))
		},
	)
	if err != nil {
		return err
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

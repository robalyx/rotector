package ai

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/alpkeskin/gotoon"
	"github.com/bytedance/sonic"
	"github.com/openai/openai-go"
	"github.com/robalyx/rotector/internal/ai/client"
	"github.com/robalyx/rotector/internal/setup"
	"github.com/robalyx/rotector/pkg/utils"
	"go.uber.org/zap"
	"golang.org/x/sync/semaphore"
)

// MessageContent represents a Discord message for analysis.
type MessageContent struct {
	MessageID string `json:"messageId"`
	Content   string `json:"content"`
}

// FlaggedMessage contains information about an inappropriate message.
type FlaggedMessage struct {
	MessageID  string  `json:"messageId"  jsonschema:"required,minLength=1,description=Discord message ID"`
	Content    string  `json:"content"    jsonschema:"required,minLength=1,description=Message content that violates policies"`
	Reason     string  `json:"reason"     jsonschema:"required,minLength=1,description=Reason for flagging this message"`
	Confidence float64 `json:"confidence" jsonschema:"required,minimum=0,maximum=1,description=Confidence score for this violation"`
}

// FlaggedMessageUser contains information about a user with inappropriate messages.
type FlaggedMessageUser struct {
	Reason     string           `json:"reason"     jsonschema:"required,minLength=1,description=Overall reason for flagging this user"`
	Messages   []FlaggedMessage `json:"messages"   jsonschema:"required,description=List of inappropriate messages from this user"`
	Confidence float64          `json:"confidence" jsonschema:"required,minimum=0,maximum=1,description=Overall confidence score for user violations"`
}

// MessageAnalyzer processes Discord messages to detect inappropriate content.
type MessageAnalyzer struct {
	chat          client.ChatCompletions
	analysisSem   *semaphore.Weighted
	logger        *zap.Logger
	textLogger    *zap.Logger
	textDir       string
	model         string
	fallbackModel string
}

// MessageAnalysisSchema is the JSON schema for the message analysis response.
var MessageAnalysisSchema = utils.GenerateSchema[FlaggedMessageUser]()

// NewMessageAnalyzer creates a new message analyzer.
func NewMessageAnalyzer(app *setup.App, logger *zap.Logger) *MessageAnalyzer {
	// Get text logger
	textLogger, textDir, err := app.LogManager.GetTextLogger("message_analyzer")
	if err != nil {
		logger.Error("Failed to create text logger", zap.Error(err))
		textLogger = logger
	}

	return &MessageAnalyzer{
		chat:          app.AIClient.Chat(),
		analysisSem:   semaphore.NewWeighted(int64(app.Config.Worker.BatchSizes.MessageAnalysis)),
		logger:        logger.Named("ai_message"),
		textLogger:    textLogger,
		textDir:       textDir,
		model:         app.Config.Common.OpenAI.MessageModel,
		fallbackModel: app.Config.Common.OpenAI.MessageFallbackModel,
	}
}

// ProcessMessages analyzes a collection of Discord messages for inappropriate content.
func (a *MessageAnalyzer) ProcessMessages(
	ctx context.Context, serverID uint64, channelID uint64, serverName string, userID uint64, messages []*MessageContent,
) (*FlaggedMessageUser, error) {
	// Acquire semaphore
	if err := a.analysisSem.Acquire(ctx, 1); err != nil {
		return nil, fmt.Errorf("failed to acquire analysis semaphore: %w", err)
	}
	defer a.analysisSem.Release(1)

	// Handle content blocking
	minBatchSize := max(len(messages)/4, 1)

	var cumulativeFlaggedUser *FlaggedMessageUser

	err := utils.WithRetrySplitBatch(
		ctx, messages, len(messages), minBatchSize, utils.GetAIRetryOptions(),
		func(batch []*MessageContent) error {
			batchResult, err := a.processMessageBatch(ctx, batch)
			if err != nil {
				return err
			}

			// Skip if batch had no flagged content
			if batchResult == nil {
				return nil
			}

			// First batch result
			if cumulativeFlaggedUser == nil {
				cumulativeFlaggedUser = batchResult
				return nil
			}

			// Merge subsequent batch results
			cumulativeFlaggedUser.Messages = append(cumulativeFlaggedUser.Messages, batchResult.Messages...)

			// Keep the reason with the highest confidence
			if batchResult.Confidence > cumulativeFlaggedUser.Confidence {
				cumulativeFlaggedUser.Confidence = batchResult.Confidence
				cumulativeFlaggedUser.Reason = batchResult.Reason
			}

			return nil
		},
		func(batch []*MessageContent) {
			// Log detailed content to text logger
			a.textLogger.Warn("Content blocked in message batch",
				zap.Uint64("serverID", serverID),
				zap.String("server_name", serverName),
				zap.Uint64("userID", userID),
				zap.Int("batch_size", len(batch)),
				zap.Any("messages", batch))

			// Save blocked messages to file
			filename := fmt.Sprintf("%d_%s.txt", serverID, time.Now().Format("20060102_150405"))
			filePath := filepath.Join(a.textDir, filename)

			var buf bytes.Buffer
			for _, msg := range batch {
				buf.WriteString(fmt.Sprintf("Message ID: %s\nUser ID: %d\nContent: %s\n\n",
					msg.MessageID, userID, msg.Content))
			}

			if err := os.WriteFile(filePath, buf.Bytes(), 0o600); err != nil {
				a.textLogger.Error("Failed to save blocked messages",
					zap.Error(err),
					zap.String("path", filePath))

				return
			}

			a.textLogger.Info("Saved blocked messages",
				zap.String("path", filePath))
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to process message batch: %w", err)
	}

	// Validate message IDs
	if cumulativeFlaggedUser != nil {
		a.validateMessageOwnership(messages, cumulativeFlaggedUser)

		// If no valid messages remain after validation, return nil
		if len(cumulativeFlaggedUser.Messages) == 0 {
			cumulativeFlaggedUser = nil
		}
	}

	a.logger.Info("Message analysis completed",
		zap.Uint64("serverID", serverID),
		zap.Uint64("channelID", channelID),
		zap.Bool("user_flagged", cumulativeFlaggedUser != nil))

	return cumulativeFlaggedUser, nil
}

// processMessageBatch handles the AI analysis for a batch of messages.
func (a *MessageAnalyzer) processMessageBatch(
	ctx context.Context, batch []*MessageContent,
) (*FlaggedMessageUser, error) {
	// Convert to TOON format
	toonData, err := gotoon.Encode(batch)
	if err != nil {
		return nil, fmt.Errorf("TOON marshal error: %w", err)
	}

	// Format the prompt using the template
	prompt := fmt.Sprintf(MessageAnalysisPrompt, toonData)

	// Prepare chat completion parameters
	params := openai.ChatCompletionNewParams{
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(MessageSystemPrompt),
			openai.UserMessage(prompt),
		},
		ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONSchema: &openai.ResponseFormatJSONSchemaParam{
				JSONSchema: openai.ResponseFormatJSONSchemaJSONSchemaParam{
					Name:        "messageAnalysis",
					Description: openai.String("Analysis of Discord messages"),
					Schema:      MessageAnalysisSchema,
					Strict:      openai.Bool(true),
				},
			},
		},
		Model:               a.model,
		Temperature:         openai.Float(0.0),
		TopP:                openai.Float(0.95),
		MaxCompletionTokens: openai.Int(8192),
	}

	// Make API request
	var result FlaggedMessageUser

	err = a.chat.NewWithRetryAndFallback(ctx, params, a.fallbackModel, func(resp *openai.ChatCompletion, err error) error {
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
			a.logger.Debug("AI message analysis thought process",
				zap.String("model", resp.Model),
				zap.String("thought", thought.Raw()))
		}

		// Parse response from AI
		if err := sonic.Unmarshal([]byte(message.Content), &result); err != nil {
			return fmt.Errorf("JSON unmarshal error: %w", err)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return &result, nil
}

// validateMessageOwnership ensures flagged messages exist in the original batch.
func (a *MessageAnalyzer) validateMessageOwnership(
	messages []*MessageContent, flaggedUser *FlaggedMessageUser,
) {
	// Build a set of valid message IDs
	validMessageIDs := make(map[string]struct{}, len(messages))
	for _, msg := range messages {
		validMessageIDs[msg.MessageID] = struct{}{}
	}

	// Validate flagged messages
	validMessages := make([]FlaggedMessage, 0, len(flaggedUser.Messages))

	for _, msg := range flaggedUser.Messages {
		// Check if message exists in the original batch
		if _, exists := validMessageIDs[msg.MessageID]; exists {
			validMessages = append(validMessages, msg)
		}
	}

	// Update messages to only include valid ones
	flaggedUser.Messages = validMessages
}

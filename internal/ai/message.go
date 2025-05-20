package ai

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/bytedance/sonic"
	"github.com/robalyx/rotector/internal/ai/client"
	"github.com/robalyx/rotector/internal/setup"
	"github.com/robalyx/rotector/pkg/utils"
	"github.com/sourcegraph/conc/pool"
	"github.com/tdewolff/minify/v2"
	"github.com/tdewolff/minify/v2/json"
	"go.uber.org/zap"

	"github.com/openai/openai-go"
	"golang.org/x/sync/semaphore"
)

// MessageContent represents a Discord message for analysis.
type MessageContent struct {
	MessageID string `json:"messageId"`
	UserID    uint64 `json:"userId"`
	Content   string `json:"content"`
}

// FlaggedMessage contains information about an inappropriate message.
type FlaggedMessage struct {
	MessageID  string  `json:"messageId"`
	Content    string  `json:"content"`
	Reason     string  `json:"reason"`
	Confidence float64 `json:"confidence"`
}

// FlaggedMessageUser contains information about a user with inappropriate messages.
type FlaggedMessageUser struct {
	UserID     uint64           `json:"userId"`
	Reason     string           `json:"reason"`
	Messages   []FlaggedMessage `json:"messages"`
	Confidence float64          `json:"confidence"`
}

// FlaggedMessagesResponse represents the AI's response structure.
type FlaggedMessagesResponse struct {
	Users []FlaggedMessageUser `json:"users"`
}

// MessageAnalyzer processes Discord messages to detect inappropriate content.
type MessageAnalyzer struct {
	chat        client.ChatCompletions
	minify      *minify.M
	analysisSem *semaphore.Weighted
	batchSize   int
	logger      *zap.Logger
	textLogger  *zap.Logger
	textDir     string
	model       string
}

// MessageAnalysisSchema is the JSON schema for the message analysis response.
var MessageAnalysisSchema = utils.GenerateSchema[FlaggedMessagesResponse]()

// NewMessageAnalyzer creates a new message analyzer.
func NewMessageAnalyzer(app *setup.App, logger *zap.Logger) *MessageAnalyzer {
	m := minify.New()
	m.AddFunc("application/json", json.Minify)

	// Get text logger
	textLogger, textDir, err := app.LogManager.GetTextLogger("message_analyzer")
	if err != nil {
		logger.Error("Failed to create text logger", zap.Error(err))
		textLogger = logger
	}

	return &MessageAnalyzer{
		chat:        app.AIClient.Chat(),
		minify:      m,
		analysisSem: semaphore.NewWeighted(int64(app.Config.Worker.BatchSizes.MessageAnalysis)),
		batchSize:   app.Config.Worker.BatchSizes.MessageAnalysisBatch,
		logger:      logger.Named("ai_message"),
		textLogger:  textLogger,
		textDir:     textDir,
		model:       app.Config.Common.OpenAI.MessageModel,
	}
}

// ProcessMessages analyzes a collection of Discord messages for inappropriate content.
func (a *MessageAnalyzer) ProcessMessages(
	ctx context.Context, serverID uint64, channelID uint64, serverName string, messages []*MessageContent,
) (map[uint64]*FlaggedMessageUser, error) {
	a.logger.Info("Starting message analysis",
		zap.Uint64("server_id", serverID),
		zap.Uint64("channel_id", channelID),
		zap.String("server_name", serverName),
		zap.Int("message_count", len(messages)))

	// Organize messages into batches
	batches := make([][]*MessageContent, 0, (len(messages)+a.batchSize-1)/a.batchSize)
	for i := 0; i < len(messages); i += a.batchSize {
		end := min(i+a.batchSize, len(messages))
		batches = append(batches, messages[i:end])
	}

	// Process batches concurrently
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()

	var (
		p            = pool.New().WithErrors().WithContext(ctx)
		flaggedUsers = make(map[uint64]*FlaggedMessageUser)
		mu           sync.Mutex
	)

	for _, batch := range batches {
		batchCopy := batch
		p.Go(func(ctx context.Context) error {
			// Analyze the batch
			flaggedResults, err := a.processBatch(ctx, serverID, serverName, batchCopy)
			if err != nil {
				return fmt.Errorf("failed to process message batch: %w", err)
			}

			// Merge results into the main map
			mu.Lock()
			defer mu.Unlock()

			for _, flaggedUser := range flaggedResults.Users {
				if existing, found := flaggedUsers[flaggedUser.UserID]; found {
					// Merge messages for existing user
					existing.Messages = append(existing.Messages, flaggedUser.Messages...)
					// Update reason if confidence is higher
					if flaggedUser.Confidence > existing.Confidence {
						existing.Reason = flaggedUser.Reason
						existing.Confidence = flaggedUser.Confidence
					}
				} else {
					// Add new user
					flaggedUsers[flaggedUser.UserID] = &FlaggedMessageUser{
						UserID:     flaggedUser.UserID,
						Reason:     flaggedUser.Reason,
						Messages:   flaggedUser.Messages,
						Confidence: flaggedUser.Confidence,
					}
				}
			}
			return nil
		})
	}

	if err := p.Wait(); err != nil {
		return nil, fmt.Errorf("error in message analysis: %w", err)
	}

	a.logger.Info("Message analysis completed",
		zap.Uint64("server_id", serverID),
		zap.Uint64("channel_id", channelID),
		zap.Int("flagged_users", len(flaggedUsers)))

	return flaggedUsers, nil
}

// processMessageBatch handles the AI analysis for a batch of messages.
func (a *MessageAnalyzer) processMessageBatch(
	ctx context.Context, serverID uint64, serverName string, batch []*MessageContent,
) (*FlaggedMessagesResponse, error) {
	// Prepare message data for AI
	type ConversationAnalysisRequest struct {
		ServerName string            `json:"serverName"`
		ServerID   uint64            `json:"serverId"`
		Messages   []*MessageContent `json:"messages"`
	}

	request := ConversationAnalysisRequest{
		ServerName: serverName,
		ServerID:   serverID,
		Messages:   batch,
	}

	// Convert to JSON
	requestJSON, err := sonic.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Minify the JSON to reduce token usage
	minifiedJSON, err := a.minify.Bytes("application/json", requestJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to minify JSON: %w", err)
	}

	// Format the prompt using the template
	prompt := fmt.Sprintf(MessageAnalysisPrompt, minifiedJSON)

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
		Model:       a.model,
		Temperature: openai.Float(0.2),
		TopP:        openai.Float(0.95),
	}

	// Make API request
	var result FlaggedMessagesResponse
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
		if thought := message.JSON.ExtraFields["reasoning"]; thought.IsPresent() {
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

	return &result, err
}

// processBatch processes a batch of messages using the AI model.
func (a *MessageAnalyzer) processBatch(
	ctx context.Context, serverID uint64, serverName string, messages []*MessageContent,
) (*FlaggedMessagesResponse, error) {
	// Acquire semaphore
	if err := a.analysisSem.Acquire(ctx, 1); err != nil {
		return nil, fmt.Errorf("failed to acquire analysis semaphore: %w", err)
	}
	defer a.analysisSem.Release(1)

	// Handle content blocking
	minBatchSize := max(len(messages)/4, 1)

	var result *FlaggedMessagesResponse
	err := utils.WithRetrySplitBatch(
		ctx, messages, len(messages), minBatchSize, utils.GetAIRetryOptions(),
		func(batch []*MessageContent) error {
			var err error
			result, err = a.processMessageBatch(ctx, serverID, serverName, batch)
			return err
		},
		func(batch []*MessageContent) {
			// Log detailed content to text logger
			a.textLogger.Warn("Content blocked in message batch",
				zap.Uint64("server_id", serverID),
				zap.String("server_name", serverName),
				zap.Int("batch_size", len(batch)),
				zap.Any("messages", batch))

			// Save blocked messages to file
			filename := fmt.Sprintf("%d_%s.txt", serverID, time.Now().Format("20060102_150405"))
			filepath := filepath.Join(a.textDir, filename)

			var buf bytes.Buffer
			for _, msg := range batch {
				buf.WriteString(fmt.Sprintf("Message ID: %s\nUser ID: %d\nContent: %s\n\n",
					msg.MessageID, msg.UserID, msg.Content))
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
		return nil, err
	}

	// Validate message IDs against user IDs
	a.validateMessageOwnership(messages, result)

	return result, nil
}

// validateMessageOwnership ensures flagged messages belong to the flagged users.
func (a *MessageAnalyzer) validateMessageOwnership(
	messages []*MessageContent, flaggedResults *FlaggedMessagesResponse,
) {
	// Build a map of message ID to user ID for quick lookups
	messageToUser := make(map[string]uint64, len(messages))
	for _, msg := range messages {
		messageToUser[msg.MessageID] = msg.UserID
	}

	// For each flagged user, validate their messages
	for i := 0; i < len(flaggedResults.Users); i++ {
		user := &flaggedResults.Users[i]
		validMessages := make([]FlaggedMessage, 0, len(user.Messages))

		for _, msg := range user.Messages {
			// Check if message exists and belongs to this user
			if ownerID, exists := messageToUser[msg.MessageID]; exists && ownerID == user.UserID {
				validMessages = append(validMessages, msg)
			}
		}

		// Update user's messages to only include valid ones
		user.Messages = validMessages

		// If no valid messages remain, remove this user from the results
		if len(user.Messages) == 0 {
			flaggedResults.Users = slices.Delete(flaggedResults.Users, i, i+1)
			i-- // Adjust index since we removed an element
		}
	}
}

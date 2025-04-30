package ai

import (
	"context"
	"fmt"
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

const (
	// MessageSystemPrompt provides detailed instructions to the AI model for analyzing Discord conversations.
	MessageSystemPrompt = `Instruction:
You are an AI moderator analyzing Discord conversations in Roblox-related servers.
Your task is to identify messages that contain sexually inappropriate content.
Your analysis should be in the context of Roblox condo servers.

Input format:
{
  "serverName": "Discord server name",
  "serverId": 123456789,
  "messages": [
    {
      "messageId": "unique-message-id",
      "userId": 123456789,
      "content": "message content"
    }
  ]
}

Output format:
{
  "users": [
    {
      "userId": 123456789,
      "reason": "Clear explanation in one sentence",
      "messages": [
        {
          "messageId": "unique-message-id",
          "content": "flagged message content",
          "reason": "Specific reason this message is inappropriate",
          "confidence": 0.0-1.0
        }
      ],
      "confidence": 0.0-1.0
    }
  ]
}

Confidence levels:
0.0: No inappropriate content
0.1-0.3: Subtle inappropriate elements
0.4-0.6: Clear inappropriate content
0.7-0.8: Strong inappropriate indicators
0.9-1.0: Explicit inappropriate content

Key instructions:
1. Return messages with sexual/inappropriate content violations
2. Include exact quotes in message content
3. Set confidence based on severity, clarity, and contextual evidence
4. Skip empty messages or messages with only non-sexual offensive content
5. Focus on protecting minors from inappropriate sexual content
6. Avoid flagging messages from potential victims
7. Ignore offensive/racist content that is not sexual in nature

Instruction: Focus on detecting:
1. Sexually explicit content or references
2. Suggestive language, sexual innuendos, or double entendres
3. References to condo games or similar euphemisms related to inappropriate Roblox content
4. Coordination or planning of inappropriate activities within Roblox games
5. References to r34 content or Rule 34
6. Attempts to move conversations to DMs or "opened DMs"
7. Coded language or euphemisms for inappropriate activities
8. Requesting Discord servers known for condo content
9. References to "exclusive" or "private" game access
10. Discussions about age-restricted or adult content
11. Sharing or requesting inappropriate avatar/character modifications
12. References to inappropriate trading or exchanges
13. Sharing or requesting inappropriate scripts, game assets or models
14. References to inappropriate roleplay or ERP
15. References to inappropriate group activities
16. Requesting to look in their bio

IMPORTANT:
Roblox is primarily used by children and young teenagers.
So be especially vigilant about content that may expose minors to inappropriate material.

IGNORE:
1. Users warning others, mentioning/confronting pedophiles, expressing concern, or calling out inappropriate behavior
2. General profanity or curse words that aren't sexual in nature
3. Non-sexual bullying or harassment
4. Spam messages without inappropriate content
5. Image, game or video links without inappropriate context`

	// MessageAnalysisPrompt provides a reminder to follow system guidelines for message analysis.
	MessageAnalysisPrompt = `Analyze these messages for inappropriate content.

Remember:
1. Only flag users who post clearly inappropriate content
2. Return an empty "users" array if no inappropriate content is found
3. Follow confidence level guide strictly

Input:
%s`
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
	model       string
}

// MessageAnalysisSchema is the JSON schema for the message analysis response.
var MessageAnalysisSchema = utils.GenerateSchema[FlaggedMessagesResponse]()

// NewMessageAnalyzer creates a new message analyzer.
func NewMessageAnalyzer(app *setup.App, logger *zap.Logger) *MessageAnalyzer {
	m := minify.New()
	m.AddFunc("application/json", json.Minify)

	return &MessageAnalyzer{
		chat:        app.AIClient.Chat(),
		minify:      m,
		analysisSem: semaphore.NewWeighted(int64(app.Config.Worker.BatchSizes.MessageAnalysis)),
		batchSize:   app.Config.Worker.BatchSizes.MessageAnalysisBatch,
		logger:      logger.Named("ai_message"),
		model:       app.Config.Common.OpenAI.DefaultModel,
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

	// Make API request with retry
	return utils.WithRetry(ctx, func() (*FlaggedMessagesResponse, error) {
		resp, err := a.chat.New(ctx, openai.ChatCompletionNewParams{
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
		})
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
			a.logger.Debug("AI message analysis thought process",
				zap.String("model", resp.Model),
				zap.String("thought", thought.Raw()))
		}

		// Parse response from AI
		var result *FlaggedMessagesResponse
		if err := sonic.Unmarshal([]byte(message.Content), &result); err != nil {
			return nil, fmt.Errorf("JSON unmarshal error: %w", err)
		}

		return result, nil
	}, utils.GetAIRetryOptions())
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
	result, err := utils.WithRetrySplitBatch(
		ctx, messages, len(messages), minBatchSize,
		func(batch []*MessageContent) (*FlaggedMessagesResponse, error) {
			return a.processMessageBatch(ctx, serverID, serverName, batch)
		},
		utils.GetAIRetryOptions(), a.logger,
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

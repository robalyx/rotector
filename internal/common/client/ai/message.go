package ai

import (
	"context"
	"fmt"
	"sync"

	"github.com/bytedance/sonic"
	"github.com/google/generative-ai-go/genai"
	"github.com/robalyx/rotector/internal/common/setup"
	"github.com/robalyx/rotector/internal/common/utils"
	"github.com/sourcegraph/conc/pool"
	"github.com/tdewolff/minify/v2"
	"github.com/tdewolff/minify/v2/json"
	"go.uber.org/zap"
	"golang.org/x/sync/semaphore"
)

const (
	// MessageSystemPrompt provides detailed instructions to the AI model for analyzing Discord conversations.
	MessageSystemPrompt = `You are an AI moderator analyzing Discord conversations in Roblox-related servers.

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

Your task is to identify messages that contain sexually inappropriate content.
Your analysis should be in the context of Roblox condo servers.
You must analyze the full context of conversations to identify patterns and hidden meanings.

Focus on detecting:

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

Remember that Roblox is primarily used by children and young teenagers.
So be especially vigilant about content that may expose minors to inappropriate material.

Confidence levels:
0.0: No inappropriate content
0.1-0.3: Subtle inappropriate elements
0.4-0.6: Clear inappropriate content
0.7-0.8: Strong inappropriate indicators
0.9-1.0: Explicit inappropriate content

Key rules:
1. Return messages with sexual/inappropriate content violations
2. Include exact quotes in message content
3. Set confidence based on severity, clarity, and contextual evidence
4. Skip empty messages or messages with only non-sexual offensive content
5. Focus on protecting minors from inappropriate sexual content
6. Avoid flagging messages from potential victims
7. Ignore offensive/racist content that is not sexual in nature

Ignore:
1. Users warning others, mentioning/confronting pedophiles, expressing concern, or calling out inappropriate behavior
2. General profanity or curse words that aren't sexual in nature
3. Non-sexual bullying or harassment
4. Spam messages without inappropriate content
5. Image, game or video links without inappropriate context`

	// MessageAnalysisPrompt provides a reminder to follow system guidelines for message analysis.
	MessageAnalysisPrompt = `Analyze these messages for inappropriate content.

Reminders:
- Only flag users who post clearly inappropriate content
- Return an empty "users" array if no inappropriate content is found
- Follow confidence level guide strictly

SERVER: "%s"

MESSAGES TO ANALYZE:
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
	messageModel *genai.GenerativeModel
	minify       *minify.M
	analysisSem  *semaphore.Weighted
	batchSize    int
	logger       *zap.Logger
}

// NewMessageAnalyzer creates a new message analyzer.
func NewMessageAnalyzer(app *setup.App, logger *zap.Logger) *MessageAnalyzer {
	// Create message analysis model
	messageModel := app.GenAIClient.GenerativeModel(app.Config.Common.GeminiAI.Model)
	messageModel.SystemInstruction = genai.NewUserContent(genai.Text(MessageSystemPrompt))
	messageModel.ResponseMIMEType = ApplicationJSON
	messageModel.ResponseSchema = &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"users": {
				Type: genai.TypeArray,
				Items: &genai.Schema{
					Type: genai.TypeObject,
					Properties: map[string]*genai.Schema{
						"userId": {
							Type:        genai.TypeInteger,
							Description: "Discord user ID of the flagged user",
						},
						"reason": {
							Type:        genai.TypeString,
							Description: "Overall reason for flagging this user",
						},
						"messages": {
							Type: genai.TypeArray,
							Items: &genai.Schema{
								Type: genai.TypeObject,
								Properties: map[string]*genai.Schema{
									"messageId": {
										Type:        genai.TypeString,
										Description: "ID of the flagged message",
									},
									"content": {
										Type:        genai.TypeString,
										Description: "Content of the flagged message",
									},
									"reason": {
										Type:        genai.TypeString,
										Description: "Specific reason this message was flagged",
									},
									"confidence": {
										Type:        genai.TypeNumber,
										Description: "Confidence score for this message between 0.0 and 1.0",
									},
								},
								Required: []string{"messageId", "content", "reason", "confidence"},
							},
						},
						"confidence": {
							Type:        genai.TypeNumber,
							Description: "Overall confidence score for this user between 0.0 and 1.0",
						},
					},
					Required: []string{"userId", "reason", "messages", "confidence"},
				},
			},
		},
		Required: []string{"users"},
	}
	messageModel.SetTemperature(0.2)
	messageModel.SetTopP(0.95)
	messageModel.SetTopK(40)
	messageModel.SetMaxOutputTokens(4096)

	// Create minifier for JSON
	m := minify.New()
	m.AddFunc("application/json", json.Minify)

	return &MessageAnalyzer{
		messageModel: messageModel,
		minify:       m,
		analysisSem:  semaphore.NewWeighted(int64(app.Config.Worker.BatchSizes.MessageAnalysis)),
		batchSize:    app.Config.Worker.BatchSizes.MessageAnalysisBatch,
		logger:       logger.Named("ai_message"),
	}
}

// ProcessMessages analyzes a collection of Discord messages for inappropriate content.
func (a *MessageAnalyzer) ProcessMessages(
	ctx context.Context,
	serverID uint64,
	channelID uint64,
	serverName string,
	messages []*MessageContent,
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
	var mu sync.Mutex
	flaggedUsers := make(map[uint64]*FlaggedMessageUser)
	p := pool.New().WithErrors().WithContext(ctx)

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

// processBatch processes a batch of messages using the AI model.
func (a *MessageAnalyzer) processBatch(
	ctx context.Context,
	serverID uint64,
	serverName string,
	messages []*MessageContent,
) (*FlaggedMessagesResponse, error) {
	// Acquire semaphore to limit concurrent AI calls
	if err := a.analysisSem.Acquire(ctx, 1); err != nil {
		return nil, fmt.Errorf("failed to acquire analysis semaphore: %w", err)
	}
	defer a.analysisSem.Release(1)

	// Prepare message data for AI
	type ConversationAnalysisRequest struct {
		ServerName string            `json:"serverName"`
		ServerID   uint64            `json:"serverId"`
		Messages   []*MessageContent `json:"messages"`
	}

	request := ConversationAnalysisRequest{
		ServerName: serverName,
		ServerID:   serverID,
		Messages:   messages,
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
	prompt := fmt.Sprintf(MessageAnalysisPrompt, serverName, minifiedJSON)

	// Call the AI with retry mechanism
	flaggedResults, err := utils.WithRetry(ctx, func() (*FlaggedMessagesResponse, error) {
		response, err := a.messageModel.GenerateContent(ctx, genai.Text(prompt))
		if err != nil {
			return nil, fmt.Errorf("AI generation failed: %w", err)
		}

		if len(response.Candidates) == 0 || len(response.Candidates[0].Content.Parts) == 0 {
			return nil, fmt.Errorf("%w: no response from Gemini", ErrModelResponse)
		}

		// Parse response from AI
		responseText, ok := response.Candidates[0].Content.Parts[0].(genai.Text)
		if !ok {
			return nil, fmt.Errorf("%w: unexpected response format from AI", ErrModelResponse)
		}

		// Parse the JSON response
		var results FlaggedMessagesResponse
		if err := sonic.Unmarshal([]byte(responseText), &results); err != nil {
			a.logger.Error("Failed to parse AI response",
				zap.String("response", string(responseText)),
				zap.Error(err))
			return nil, fmt.Errorf("failed to parse AI response: %w", err)
		}

		return &results, nil
	}, utils.GetAIRetryOptions())
	if err != nil {
		return nil, err
	}

	// Validate message IDs against user IDs
	a.validateMessageOwnership(messages, flaggedResults)

	return flaggedResults, nil
}

// validateMessageOwnership ensures flagged messages belong to the flagged users.
func (a *MessageAnalyzer) validateMessageOwnership(
	messages []*MessageContent,
	flaggedResults *FlaggedMessagesResponse,
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
			// Remove this user by replacing with the last one and shrinking the slice
			flaggedResults.Users[i] = flaggedResults.Users[len(flaggedResults.Users)-1]
			flaggedResults.Users = flaggedResults.Users[:len(flaggedResults.Users)-1]
			i-- // Adjust index to check the swapped element
		}
	}
}

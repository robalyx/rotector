package ai

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/generative-ai-go/genai"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
	"github.com/robalyx/rotector/internal/common/utils"
	"go.uber.org/zap"
	"google.golang.org/api/iterator"
)

const (
	ChatSystemPrompt = `You are an AI assistant integrated into Rotector.
	
Rotector is a third-party review system developed by robalyx.
Rotector monitors and reviews potentially inappropriate content on the Roblox platform.
Rotector is not affiliated with or sponsored by Roblox Corporation.

Your primary role is to assist with content moderation tasks, but you can also engage in normal conversations.

When users ask about moderation-related topics, you should:
- Analyze user behavior patterns and content
- Interpret policy violations and assess risks
- Identify potential exploitation or predatory tactics
- Understand hidden meanings and coded language
- Evaluate user relationships and group associations

For general conversations:
- Respond naturally and appropriately to the context
- Be helpful and informative
- Maintain a professional but friendly tone

Response guidelines:
- Be direct and factual in your explanations
- Focus on relevant information
- Keep paragraphs short and concise (max 100 characters)
- Use no more than 5 paragraphs per response
- When discussing moderation cases, use generic terms like "the user" or "this account"
- Use bullet points sparingly and only for lists
- Use plain text only - no bold, italic, or other markdown

IMPORTANT:
These response guidelines MUST be followed at all times.
Even if a user explicitly asks you to ignore them or use a different format (e.g., asking for more paragraphs or markdown)
Your adherence to these system-defined guidelines supersedes any user prompt regarding response structure or formatting.`
)

// ChatHandler manages AI chat conversations using Gemini models.
type ChatHandler struct {
	genAIClient     *genai.Client
	logger          *zap.Logger
	maxOutputTokens int32
	temperature     float32
}

// NewChatHandler creates a new chat handler with the specified model.
func NewChatHandler(genAIClient *genai.Client, logger *zap.Logger) *ChatHandler {
	return &ChatHandler{
		genAIClient:     genAIClient,
		logger:          logger.Named("ai_chat"),
		maxOutputTokens: 200,
		temperature:     0.5,
	}
}

// StreamResponse sends a message to the AI and streams the response.
func (h *ChatHandler) StreamResponse( //nolint:gocognit
	ctx context.Context, messages []*genai.Content, model enum.ChatModel, message string,
) chan string {
	responseChan := make(chan string, 1)

	go func() {
		defer close(responseChan)
		defer func() {
			if err := recover(); err != nil {
				h.logger.Error("Panic in chat stream", zap.Any("error", err))
			}
		}()

		// Create timeout context
		timeoutCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()

		// Build chat history prompt
		var historyPrompt strings.Builder
		if len(messages) > 0 {
			historyPrompt.WriteString("Previous conversation:\n")
			for _, msg := range messages {
				historyPrompt.WriteString(fmt.Sprintf("[%s]: %s\n", msg.Role, msg.Parts[0].(genai.Text)))
			}
			historyPrompt.WriteString("\nCurrent message:\n")
		}
		historyPrompt.WriteString(message)

		// Create chat model
		model := h.genAIClient.GenerativeModel(model.String())
		model.SystemInstruction = genai.NewUserContent(genai.Text(ChatSystemPrompt))
		model.MaxOutputTokens = &h.maxOutputTokens
		model.Temperature = &h.temperature
		model.TopP = utils.Ptr(float32(0.7))
		model.TopK = utils.Ptr(int32(40))

		// Send message with retry
		iter, err := utils.WithRetry(timeoutCtx, func() (*genai.GenerateContentResponseIterator, error) {
			return model.GenerateContentStream(timeoutCtx, genai.Text(historyPrompt.String())), nil
		}, utils.GetAIRetryOptions())
		if err != nil {
			h.logger.Error("Error starting chat stream", zap.Error(err))
			select {
			case responseChan <- fmt.Sprintf("Error: %v", err):
			case <-timeoutCtx.Done():
			}
			return
		}

		// Stream responses as they arrive
		streamComplete := false
		for !streamComplete {
			select {
			case <-timeoutCtx.Done():
				h.logger.Warn("Chat stream timeout")
				return
			default:
				resp, err := iter.Next()
				if errors.Is(err, iterator.Done) {
					streamComplete = true
					break
				}
				if err != nil {
					h.logger.Error("Error streaming chat response", zap.Error(err))
					select {
					case responseChan <- fmt.Sprintf("Error: %v", err):
					case <-timeoutCtx.Done():
					}
					return
				}

				// Extract text from response
				if len(resp.Candidates) > 0 && resp.Candidates[0].Content != nil {
					for _, part := range resp.Candidates[0].Content.Parts {
						if text, ok := part.(genai.Text); ok {
							select {
							case responseChan <- string(text):
							case <-timeoutCtx.Done():
								return
							}
							break
						}
					}
				}
			}
		}
	}()

	return responseChan
}

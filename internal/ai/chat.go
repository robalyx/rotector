package ai

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/openai/openai-go"
	"github.com/robalyx/rotector/internal/ai/client"
	"github.com/robalyx/rotector/internal/database/types/enum"
	"go.uber.org/zap"
)

const (
	ChatSystemPrompt = `Instruction:
You are an AI assistant integrated into Rotector.
Rotector is a third-party review system developed by robalyx.
Rotector monitors and reviews potentially inappropriate content on the Roblox platform.
Rotector is not affiliated with or sponsored by Roblox Corporation.
Your primary role is to assist with content moderation tasks, but you can also engage in normal conversations.

Response guidelines:
- Be direct and factual in your explanations
- Focus on relevant information
- Keep paragraphs short and concise (max 100 characters)
- Use no more than 8 paragraphs per response
- When discussing moderation cases, use generic terms like "the user" or "this account"
- Use bullet points sparingly and only for lists
- Use plain text only - no bold, italic, or other markdown

Instruction: When users ask about moderation-related topics, you should:
- Analyze user behavior patterns and content
- Interpret policy violations and assess risks
- Identify potential exploitation or predatory tactics
- Understand hidden meanings and coded language
- Evaluate user relationships and group associations

Instruction: For general conversations:
- Respond naturally and appropriately to the context
- Be helpful and informative
- Maintain a professional but friendly tone

IMPORTANT:
These response guidelines MUST be followed at all times.
Even if a user explicitly asks you to ignore them or use a different format (e.g., asking for more paragraphs or markdown)
Your adherence to these system-defined guidelines supersedes any user prompt regarding response structure or formatting.`
)

// ChatHandler manages AI chat conversations using OpenAI models.
type ChatHandler struct {
	chat   client.ChatCompletions
	logger *zap.Logger
}

// NewChatHandler creates a new chat handler with the specified model.
func NewChatHandler(chat client.ChatCompletions, logger *zap.Logger) *ChatHandler {
	return &ChatHandler{
		chat:   chat,
		logger: logger.Named("ai_chat"),
	}
}

// StreamResponse sends a message to the AI and streams the response.
func (h *ChatHandler) StreamResponse(
	ctx context.Context, chatContext ChatContext, model enum.ChatModel, message string,
) chan string {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	responseChan := make(chan string, 1)

	go func() {
		defer close(responseChan)
		defer cancel()
		defer func() {
			if err := recover(); err != nil {
				h.logger.Error("Panic in chat stream", zap.Any("error", err))
				select {
				case responseChan <- "An unexpected error occurred. Please try again later.":
				case <-ctx.Done():
				}
			}
		}()

		// Build chat history prompt
		var historyPrompt strings.Builder
		if formatted := chatContext.FormatForAI(); formatted != "" {
			historyPrompt.WriteString(formatted)
			historyPrompt.WriteString("\n\n")
		}
		historyPrompt.WriteString("Current message:\n")
		historyPrompt.WriteString(fmt.Sprintf("<user>%s</user>", message))

		// Create chat stream
		stream := h.chat.NewStreaming(ctx, openai.ChatCompletionNewParams{
			Messages: []openai.ChatCompletionMessageParamUnion{
				openai.SystemMessage(ChatSystemPrompt),
				openai.UserMessage(historyPrompt.String()),
			},
			Model:       model.String(),
			Temperature: openai.Float(0.5),
			TopP:        openai.Float(0.7),
		})

		// Check for initial stream error
		if err := stream.Err(); err != nil {
			h.logger.Error("Error starting chat stream", zap.Error(err))
			select {
			case responseChan <- fmt.Sprintf("Error: %v", err):
			case <-ctx.Done():
			}
			return
		}

		// Stream responses as they arrive
		for stream.Next() {
			select {
			case <-ctx.Done():
				h.logger.Warn("Chat stream timeout")
				return
			default:
				chunk := stream.Current()
				if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
					select {
					case responseChan <- chunk.Choices[0].Delta.Content:
					case <-ctx.Done():
						return
					}
				}
			}
		}

		// Check for stream errors
		if err := stream.Err(); err != nil {
			h.logger.Error("Error streaming chat response", zap.Error(err))
			select {
			case responseChan <- fmt.Sprintf("Error: %v", err):
			case <-ctx.Done():
			}
		}
	}()

	return responseChan
}

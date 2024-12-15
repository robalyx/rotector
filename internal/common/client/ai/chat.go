package ai

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/generative-ai-go/genai"
	"go.uber.org/zap"
	"google.golang.org/api/iterator"
)

const (
	ChatSystemPrompt = `You are an AI assistant integrated into Rotector.
	
Rotector is a third-party review system developed by jaxron for monitoring and reviewing potentially inappropriate content on the Roblox platform.
Rotector is not affiliated with or sponsored by Roblox Corporation.

Rotector uses AI techniques to flag suspicious users, and you are here to assist human moderators in:
- Analyzing user behavior patterns and content
- Interpreting policy violations and assessing risks
- Providing guidance on moderation best practices
- Identifying potential exploitation or predatory tactics
- Understanding hidden meanings and coded language
- Evaluating user relationships and group associations

You should:
- Keep responses brief and concise (max 2-3 short paragraphs)
- Be direct and factual in your explanations
- Focus on the most relevant information for moderation
- Highlight key risks and concerns
- Suggest practical moderation actions when needed

Format your responses:
- Use paragraphs to separate different points
- Start new paragraphs for new topics
- Keep paragraphs focused and concise
- Use bullet points sparingly and only for lists
- Use plain text only - no bold, italic, or other markdown`
)

// ChatHandler manages AI chat conversations using Gemini models.
type ChatHandler struct {
	genAIClient     *genai.Client
	logger          *zap.Logger
	maxOutputTokens int32
}

// NewChatHandler creates a new chat handler with the specified model.
func NewChatHandler(genAIClient *genai.Client, logger *zap.Logger) *ChatHandler {
	return &ChatHandler{
		genAIClient:     genAIClient,
		logger:          logger,
		maxOutputTokens: 200,
	}
}

// StreamResponse sends a message to the AI and streams the response through a channel.
func (h *ChatHandler) StreamResponse(ctx context.Context, history []*genai.Content, modelName string, message string) chan string {
	responseChan := make(chan string)
	go func() {
		defer close(responseChan)

		// Create chat model
		model := h.genAIClient.GenerativeModel(modelName)
		model.SystemInstruction = genai.NewUserContent(genai.Text(ChatSystemPrompt))
		model.MaxOutputTokens = &h.maxOutputTokens

		// Create chat session with history
		cs := model.StartChat()
		cs.History = history

		// Send message and get response stream
		iter := cs.SendMessageStream(ctx, genai.Text(message))

		// Stream responses as they arrive
		for {
			resp, err := iter.Next()
			if errors.Is(err, iterator.Done) {
				break
			}
			if err != nil {
				h.logger.Error("Error streaming chat response", zap.Error(err))
				responseChan <- fmt.Sprintf("Error: %v", err)
				return
			}

			// Extract text from response
			for _, cand := range resp.Candidates {
				if cand.Content != nil {
					for _, part := range cand.Content.Parts {
						if text, ok := part.(genai.Text); ok {
							responseChan <- string(text)
						}
					}
				}
			}
		}
	}()

	return responseChan
}

package ai

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/google/generative-ai-go/genai"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
	"github.com/robalyx/rotector/internal/common/utils"
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
- Be direct and factual in your explanations
- Focus on the most relevant information for moderation
- Highlight key risks and concerns

Format your responses:
- Use paragraphs to separate different points
- Each paragraph is short and concise (max 100 characters)
- Ensure there are no more than 3 paragraphs
- Use bullet points sparingly and only for lists
- Use plain text only - no bold, italic, or other markdown`
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
		logger:          logger,
		maxOutputTokens: 200,
		temperature:     0.5,
	}
}

// StreamResponse sends a message to the AI and streams both the response and history through channels.
func (h *ChatHandler) StreamResponse(ctx context.Context, history []*genai.Content, model enum.ChatModel, message string) (chan string, chan []*genai.Content) {
	responseChan := make(chan string)
	historyChan := make(chan []*genai.Content, 1)

	go func() {
		defer close(responseChan)
		defer close(historyChan)

		// Limit history to last 6 messages
		limitedHistory := history
		if len(history) > 6 {
			limitedHistory = history[len(history)-6:]
		}

		// Create chat model
		cc, err := h.genAIClient.CreateCachedContent(ctx, &genai.CachedContent{
			Model:             model.String(),
			SystemInstruction: genai.NewUserContent(genai.Text(ChatSystemPrompt)),
			Contents:          limitedHistory,
		})
		if err != nil {
			log.Fatal(err)
		}
		defer func() {
			if err := h.genAIClient.DeleteCachedContent(ctx, cc.Name); err != nil {
				h.logger.Error("Error deleting cached content", zap.Error(err))
			}
		}()

		model := h.genAIClient.GenerativeModelFromCachedContent(cc)
		model.MaxOutputTokens = &h.maxOutputTokens
		model.Temperature = &h.temperature
		model.TopP = utils.Ptr(float32(0.7))
		model.TopK = utils.Ptr(int32(40))

		// Create chat session with history
		cs := model.StartChat()

		// Send message with retry
		iter, err := withRetry(ctx, func() (*genai.GenerateContentResponseIterator, error) {
			return cs.SendMessageStream(ctx, genai.Text(message)), nil
		})
		if err != nil {
			h.logger.Error("Error starting chat stream", zap.Error(err))
			responseChan <- fmt.Sprintf("Error: %v", err)
			return
		}

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

		// Send final history after conversation is complete
		historyChan <- cs.History
	}()

	return responseChan, historyChan
}

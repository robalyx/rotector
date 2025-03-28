package ai

import (
	"errors"
	"fmt"
	"strings"

	"github.com/google/generative-ai-go/genai"
)

const (
	// ApplicationJSON is the MIME type for JSON content.
	ApplicationJSON = "application/json"
	// TextPlain is the MIME type for plain text content.
	TextPlain = "text/plain"
)

var (
	// ErrModelResponse indicates the model returned no usable response.
	ErrModelResponse = errors.New("model response error")
	// ErrJSONProcessing indicates a JSON processing error.
	ErrJSONProcessing = errors.New("JSON processing error")
)

// ContextType represents the type of message context.
type ContextType string

const (
	ContextTypeUser  ContextType = "user"
	ContextTypeGroup ContextType = "group"
	ContextTypeHuman ContextType = "human"
	ContextTypeAI    ContextType = "ai"
)

// ContextMap is a map of context types to their corresponding contexts.
type ContextMap map[ContextType][]Context

// ChatContext is a slice of ordered contexts.
type ChatContext []Context

// Context represents a single context entry in the chat history.
type Context struct {
	Type    ContextType
	Content string
	Model   string
}

// GetRecentMessages returns the most recent chat messages for the AI.
func (cc ChatContext) GetRecentMessages() []*genai.Content {
	messages := make([]*genai.Content, 0, len(cc))

	// Convert all contexts to messages
	for _, ctx := range cc {
		var role string
		switch ctx.Type {
		case ContextTypeAI:
			role = "model"
		case ContextTypeUser, ContextTypeGroup, ContextTypeHuman:
			role = "user"
		}

		messages = append(messages, &genai.Content{
			Role:  role,
			Parts: []genai.Part{genai.Text(ctx.Content)},
		})
	}

	// Keep only the last 10 messages
	if len(messages) > 10 {
		messages = messages[len(messages)-10:]
	}
	return messages
}

// FormatForAI formats the context for inclusion in AI messages.
func (cc ChatContext) FormatForAI() string {
	if len(cc) == 0 {
		return ""
	}

	var b strings.Builder
	for i, ctx := range cc {
		// Skip chat messages in the context formatting
		if ctx.Type == ContextTypeHuman || ctx.Type == ContextTypeAI {
			continue
		}

		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(fmt.Sprintf("=== %s Context ===\n", strings.ToUpper(string(ctx.Type))))
		b.WriteString(ctx.Content)
	}
	return b.String()
}

// GroupByType converts a ChatContext slice into a ContextMap.
func (cc ChatContext) GroupByType() ContextMap {
	grouped := make(ContextMap)
	for _, ctx := range cc {
		grouped[ctx.Type] = append(grouped[ctx.Type], ctx)
	}
	return grouped
}

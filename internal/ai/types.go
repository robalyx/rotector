package ai

import (
	"errors"
	"fmt"
	"strings"
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

// FormatForAI formats the context for inclusion in AI messages.
func (cc ChatContext) FormatForAI() string {
	if len(cc) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("Previous conversation:\n")

	for _, ctx := range cc {
		switch ctx.Type {
		case ContextTypeHuman:
			b.WriteString(fmt.Sprintf("<previous user>%s</previous>\n", ctx.Content))
		case ContextTypeAI:
			b.WriteString(fmt.Sprintf("<previous assistant>%s</previous>\n", ctx.Content))
		case ContextTypeUser, ContextTypeGroup:
			b.WriteString(fmt.Sprintf("<context %s>\n%s\n</context>\n", strings.ToLower(string(ctx.Type)), ctx.Content))
		}
	}

	return strings.TrimSpace(b.String())
}

// GroupByType converts a ChatContext slice into a ContextMap.
func (cc ChatContext) GroupByType() ContextMap {
	grouped := make(ContextMap)
	for _, ctx := range cc {
		grouped[ctx.Type] = append(grouped[ctx.Type], ctx)
	}
	return grouped
}

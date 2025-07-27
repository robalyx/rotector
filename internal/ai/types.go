package ai

import (
	"fmt"
	"strings"
)

const (
	// ApplicationJSON is the MIME type for JSON content.
	ApplicationJSON = "application/json"
	// TextPlain is the MIME type for plain text content.
	TextPlain = "text/plain"
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
		content := ctx.Content

		// For AI responses, remove thinking blocks
		if ctx.Type == ContextTypeAI {
			for {
				startIdx := strings.Index(content, "<think>")
				if startIdx == -1 {
					break
				}

				endIdx := strings.Index(content[startIdx:], "</think>")
				if endIdx == -1 {
					break
				}

				endIdx += startIdx + 8 // Add length of "</think>"
				content = content[:startIdx] + content[endIdx:]
			}

			content = strings.TrimSpace(content)
		}

		switch ctx.Type {
		case ContextTypeHuman:
			b.WriteString(fmt.Sprintf("<previous user>%s</previous>\n", content))
		case ContextTypeAI:
			if content != "" {
				b.WriteString(fmt.Sprintf("<previous assistant>%s</previous>\n", content))
			}
		case ContextTypeUser, ContextTypeGroup:
			b.WriteString(fmt.Sprintf("<context %s>\n%s\n</context>\n", strings.ToLower(string(ctx.Type)), content))
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

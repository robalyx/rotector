package ai

import (
	"errors"

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

// ChatMessage represents a single message in the chat history.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ToChatContent converts a ChatMessage to genai.Content.
func (m *ChatMessage) ToChatContent() *genai.Content {
	return &genai.Content{
		Parts: []genai.Part{genai.Text(m.Content)},
		Role:  m.Role,
	}
}

// ChatHistory represents the full chat history that can be stored in session.
type ChatHistory struct {
	Messages []*ChatMessage `json:"messages"`
}

// ToGenAIHistory converts ChatHistory to a slice of genai.Content.
func (h *ChatHistory) ToGenAIHistory() []*genai.Content {
	// Keep last 10 messages maximum
	start := len(h.Messages)
	if start > 10 {
		start = len(h.Messages) - 10
	}

	// Convert messages to genai.Content
	contents := make([]*genai.Content, 0, 10)
	for _, msg := range h.Messages[start:] {
		if content := msg.ToChatContent(); content != nil {
			contents = append(contents, content)
		}
	}

	return contents
}

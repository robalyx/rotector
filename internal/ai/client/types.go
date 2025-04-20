package client

import (
	"context"
	"time"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/packages/ssestream"
	"github.com/sony/gobreaker"
	"golang.org/x/sync/semaphore"
)

// ReasoningOptions contains options for configuring reasoning fields.
type ReasoningOptions struct {
	// Effort specifies the level of reasoning effort (low, medium, high).
	Effort openai.ReasoningEffort
	// MaxTokens specifies the maximum number of tokens for reasoning.
	MaxTokens int
	// Exclude determines whether to exclude reasoning from the response.
	Exclude bool
}

// BlockReason represents different types of content blocking reasons.
type BlockReason string

const (
	BlockReasonUnspecified BlockReason = "BLOCK_REASON_UNSPECIFIED"
	BlockReasonSafety      BlockReason = "SAFETY"
	BlockReasonOther       BlockReason = "OTHER"
	BlockReasonBlocklist   BlockReason = "BLOCKLIST"
	BlockReasonProhibited  BlockReason = "PROHIBITED_CONTENT"
	BlockReasonImageSafety BlockReason = "IMAGE_SAFETY"
)

// CircuitBreakerSettings contains configuration for the circuit breaker.
type CircuitBreakerSettings struct {
	MaxFailures     uint32
	Timeout         time.Duration
	HalfOpenMaxReqs uint32
}

// Client provides a unified interface for making AI requests.
type Client interface {
	Chat() ChatCompletions
}

// ChatCompletions provides chat completion methods.
type ChatCompletions interface {
	New(ctx context.Context, params openai.ChatCompletionNewParams) (*openai.ChatCompletion, error)
	NewStreaming(ctx context.Context, params openai.ChatCompletionNewParams) *ssestream.Stream[openai.ChatCompletionChunk]
}

// providerClient represents a single provider's client and configuration.
type providerClient struct {
	client        *openai.Client
	breaker       *gobreaker.CircuitBreaker
	semaphore     *semaphore.Weighted
	modelMappings map[string]string
	name          string
}

// String returns the string representation of the block reason.
func (br BlockReason) String() string {
	return string(br)
}

// Details returns human-readable details about the block reason.
func (br BlockReason) Details() string {
	switch br {
	case BlockReasonSafety:
		return "Check safetyRatings for specific category"
	case BlockReasonBlocklist:
		return "Content matched terminology blocklist"
	case BlockReasonImageSafety:
		return "Unsafe image generation content"
	case BlockReasonProhibited:
		return "Content violates provider's content policy"
	case BlockReasonOther:
		return "Content blocked for unspecified reasons"
	case BlockReasonUnspecified:
		return "Default block reason (unused)"
	}
	return "Unknown block reason"
}

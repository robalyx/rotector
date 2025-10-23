package client

import (
	"context"
	"time"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/packages/ssestream"
)

// RetryCallback is a function that will be called on each attempt.
type RetryCallback func(resp *openai.ChatCompletion, err error) error

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
	NewWithRetry(ctx context.Context, params openai.ChatCompletionNewParams, callback RetryCallback) error
	NewWithRetryAndFallback(ctx context.Context, params openai.ChatCompletionNewParams, fallbackModel string, callback RetryCallback) error
	NewStreaming(ctx context.Context, params openai.ChatCompletionNewParams) *ssestream.Stream[openai.ChatCompletionChunk]
}

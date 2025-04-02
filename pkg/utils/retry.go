package utils

import (
	"context"
	"time"

	"github.com/cenkalti/backoff/v4"
)

// RetryOptions contains configuration for retry behavior.
type RetryOptions struct {
	MaxElapsedTime  time.Duration
	InitialInterval time.Duration
	MaxInterval     time.Duration
	MaxRetries      uint64
}

// GetAIRetryOptions returns retry options optimized for AI operations.
func GetAIRetryOptions() RetryOptions {
	return RetryOptions{
		MaxElapsedTime:  30 * time.Second,
		InitialInterval: 100 * time.Millisecond,
		MaxInterval:     1 * time.Second,
		MaxRetries:      3,
	}
}

// GetThumbnailRetryOptions returns retry options optimized for thumbnail fetching.
func GetThumbnailRetryOptions() RetryOptions {
	return RetryOptions{
		MaxElapsedTime:  20 * time.Second,
		InitialInterval: 5 * time.Second,
		MaxInterval:     6 * time.Second,
		MaxRetries:      3,
	}
}

// WithRetry executes the given operation with exponential backoff using provided options.
func WithRetry[T any](ctx context.Context, operation func() (T, error), opts RetryOptions) (T, error) {
	var result T

	// Configure exponential backoff
	b := backoff.WithMaxRetries(backoff.NewExponentialBackOff(
		backoff.WithMaxElapsedTime(opts.MaxElapsedTime),
		backoff.WithInitialInterval(opts.InitialInterval),
		backoff.WithMaxInterval(opts.MaxInterval),
	), opts.MaxRetries)

	// Create backoff operation with context
	backoffOperation := func() error {
		var err error
		result, err = operation()
		return err
	}

	err := backoff.Retry(backoffOperation, backoff.WithContext(b, ctx))
	return result, err
}

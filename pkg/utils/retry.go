package utils

import (
	"context"
	"errors"
	"time"

	"github.com/cenkalti/backoff/v4"
)

// ErrContentBlocked is returned when content is blocked by AI safety filters.
var ErrContentBlocked = errors.New("content blocked by AI safety filters")

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
		InitialInterval: 5 * time.Second,
		MaxInterval:     10 * time.Second,
		MaxRetries:      3,
	}
}

// GetThumbnailRetryOptions returns retry options optimized for thumbnail fetching.
func GetThumbnailRetryOptions() RetryOptions {
	return RetryOptions{
		MaxElapsedTime:  60 * time.Second,
		InitialInterval: 5 * time.Second,
		MaxInterval:     20 * time.Second,
		MaxRetries:      5,
	}
}

// WithRetry executes the given operation with exponential backoff using provided options.
func WithRetry(ctx context.Context, operation func() error, opts RetryOptions) error {
	b := backoff.WithMaxRetries(backoff.NewExponentialBackOff(
		backoff.WithMaxElapsedTime(opts.MaxElapsedTime),
		backoff.WithInitialInterval(opts.InitialInterval),
		backoff.WithMaxInterval(opts.MaxInterval),
	), opts.MaxRetries)

	return backoff.Retry(operation, backoff.WithContext(b, ctx))
}

// WithRetrySplitBatch executes an operation with batch splitting on content block.
// If content is blocked, it recursively splits the batch until reaching minBatchSize.
func WithRetrySplitBatch[T any](
	ctx context.Context, items []T, batchSize int, minBatchSize int, opts RetryOptions,
	operation func([]T) error, onBlocked func([]T),
) error {
	// Base case: batch size too small, skip splitting
	if len(items) <= minBatchSize {
		// At min batch size, just run the operation directly without retry
		err := operation(items)
		if errors.Is(err, ErrContentBlocked) && onBlocked != nil {
			onBlocked(items)
		}
		return err
	}

	// Try processing the full batch once to see if it's blocked
	err := operation(items)

	// If success or not a content block error, return as is
	if err == nil || !errors.Is(err, ErrContentBlocked) {
		return err
	}

	// Content was blocked, split batch and try each half
	mid := len(items) / 2
	newBatchSize := batchSize / 2

	// Process first half
	firstErr := WithRetrySplitBatch(ctx, items[:mid], newBatchSize, minBatchSize, opts, operation, onBlocked)

	// Process second half
	secondErr := WithRetrySplitBatch(ctx, items[mid:], newBatchSize, minBatchSize, opts, operation, onBlocked)

	// If both halves failed with content block, propagate the error
	if errors.Is(firstErr, ErrContentBlocked) && errors.Is(secondErr, ErrContentBlocked) {
		return ErrContentBlocked
	}

	// If one half succeeded, return nil
	if firstErr == nil || secondErr == nil {
		return nil
	}

	// If both halves failed but not due to content blocking, return the first error
	return firstErr
}

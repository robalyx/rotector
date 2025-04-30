package utils

import (
	"context"
	"errors"
	"time"

	"github.com/cenkalti/backoff/v4"
	"go.uber.org/zap"
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
		MaxElapsedTime:  120 * time.Second,
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

// WithRetrySplitBatch executes an operation with batch splitting on content block.
// If content is blocked, it recursively splits the batch until reaching minBatchSize.
func WithRetrySplitBatch[T, R any](
	ctx context.Context, items []T, batchSize int, minBatchSize int,
	operation func([]T) (R, error), opts RetryOptions, logger *zap.Logger,
) (R, error) {
	var empty R

	// Base case: batch size too small, skip splitting
	if len(items) <= minBatchSize {
		// At min batch size, just run the operation directly without retry
		return operation(items)
	}

	// Try processing the full batch once to see if it's blocked
	result, err := operation(items)

	// If success or not a content block error, return as is
	if err == nil || !errors.Is(err, ErrContentBlocked) {
		return result, err
	}

	// Content was blocked, split batch and try each half
	mid := len(items) / 2
	newBatchSize := batchSize / 2

	if logger != nil {
		logger.Debug("Splitting batch due to content block",
			zap.Int("originalSize", len(items)),
			zap.Int("newSize", newBatchSize),
			zap.Int("minBatchSize", minBatchSize))
	}

	// Process first half
	result, err = WithRetrySplitBatch(ctx, items[:mid], newBatchSize, minBatchSize, operation, opts, logger)
	if err == nil {
		return result, nil
	}

	// If first half failed but not due to content blocking, return that error
	if !errors.Is(err, ErrContentBlocked) {
		return empty, err
	}

	// If first half failed due to content blocking, try second half
	return WithRetrySplitBatch(ctx, items[mid:], newBatchSize, minBatchSize, operation, opts, logger)
}

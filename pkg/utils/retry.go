package utils

import (
	"context"
	"errors"
	"time"

	"github.com/cenkalti/backoff/v4"
)

var (
	// ErrContentBlocked is returned when content is blocked by AI safety filters.
	ErrContentBlocked = errors.New("content blocked by AI safety filters")
	// ErrModelResponse indicates the model returned no usable response.
	ErrModelResponse = errors.New("model response error")
	// ErrJSONProcessing indicates a JSON processing error.
	ErrJSONProcessing = errors.New("JSON processing error")
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
		MaxElapsedTime:  40 * time.Second,
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
// If content is blocked, it recursively splits the batch until reaching minBatchSize,
// then processes each item individually to maximize successful processing.
func WithRetrySplitBatch[T any](
	ctx context.Context, items []T, batchSize int, minBatchSize int, opts RetryOptions,
	operation func([]T) error, onBlocked func([]T),
) error {
	var allBlockedItems []T

	// Execute the recursive splitting logic
	err := withRetrySplitBatchInternal(ctx, items, batchSize, minBatchSize, opts, operation, &allBlockedItems)

	// Call onBlocked with all blocked items after operation completes
	if len(allBlockedItems) > 0 && onBlocked != nil {
		onBlocked(allBlockedItems)
	}

	return err
}

// withRetrySplitBatchInternal handles the recursive logic without calling onBlocked.
func withRetrySplitBatchInternal[T any](
	ctx context.Context, items []T, batchSize int, minBatchSize int, opts RetryOptions,
	operation func([]T) error, allBlockedItems *[]T,
) error {
	// Base case: process items individually at minimum batch size
	if len(items) <= minBatchSize {
		var lastNonBlockedErr error

		successCount := 0
		blockedCount := 0

		// Process each item individually to maximize success rate
		for _, item := range items {
			err := operation([]T{item})

			switch {
			case err == nil:
				successCount++
			case errors.Is(err, ErrContentBlocked):
				blockedCount++
				// Accumulate blocked items for final callback
				*allBlockedItems = append(*allBlockedItems, item)
			default:
				// Save first non-blocking error for potential return
				if lastNonBlockedErr == nil {
					lastNonBlockedErr = err
				}
			}
		}

		// Return partial success if any items succeeded
		switch {
		case successCount > 0:
			return nil
		case blockedCount > 0 && lastNonBlockedErr == nil:
			// All items blocked by content filter
			return ErrContentBlocked
		default:
			// All items failed with non-blocking errors
			return lastNonBlockedErr
		}
	}

	// Try processing the full batch first
	err := operation(items)
	if err == nil || !errors.Is(err, ErrContentBlocked) {
		return err
	}

	// Content blocked, split batch in half and recurse
	mid := len(items) / 2
	newBatchSize := batchSize / 2

	// Process both halves recursively
	firstErr := withRetrySplitBatchInternal[T](ctx, items[:mid], newBatchSize, minBatchSize, opts, operation, allBlockedItems)
	secondErr := withRetrySplitBatchInternal[T](ctx, items[mid:], newBatchSize, minBatchSize, opts, operation, allBlockedItems)

	// Return content blocked only if both halves were completely blocked
	if errors.Is(firstErr, ErrContentBlocked) && errors.Is(secondErr, ErrContentBlocked) {
		return ErrContentBlocked
	}

	// Return success if at least one half succeeded
	if firstErr == nil || secondErr == nil {
		return nil
	}

	// Both halves failed with non-blocking errors
	return firstErr
}

package utils_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/robalyx/rotector/pkg/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

var (
	errTemporary    = errors.New("temporary error")
	errOther        = errors.New("operation failed")
	errMinBatchSize = errors.New("failed at minimum batch size")
)

func TestWithRetry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		operation     func() error
		expectedCalls int
		expectedErr   error
	}{
		{
			name: "succeeds first try",
			operation: func() error {
				return nil
			},
			expectedCalls: 1,
			expectedErr:   nil,
		},
		{
			name: "succeeds after retries",
			operation: func() func() error {
				count := 0
				return func() error {
					count++
					if count < 3 {
						return errTemporary
					}
					return nil
				}
			}(),
			expectedCalls: 3,
			expectedErr:   nil,
		},
		{
			name: "fails all retries",
			operation: func() error {
				return errTemporary
			},
			expectedCalls: 4, // Initial + 3 retries
			expectedErr:   errTemporary,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()
			calls := 0
			wrappedOp := func() error {
				calls++
				return tt.operation()
			}

			opts := utils.RetryOptions{
				MaxElapsedTime:  100 * time.Millisecond,
				InitialInterval: 10 * time.Millisecond,
				MaxInterval:     20 * time.Millisecond,
				MaxRetries:      3,
			}

			err := utils.WithRetry(ctx, wrappedOp, opts)

			if tt.expectedErr != nil {
				require.Error(t, err)
				require.ErrorIs(t, err, tt.expectedErr)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tt.expectedCalls, calls)
		})
	}
}

func TestWithRetryContext(t *testing.T) {
	t.Parallel()

	t.Run("respects context cancellation", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(t.Context())
		calls := 0

		operation := func() error {
			calls++
			return errTemporary
		}

		opts := utils.RetryOptions{
			MaxElapsedTime:  1 * time.Second,
			InitialInterval: 100 * time.Millisecond,
			MaxInterval:     200 * time.Millisecond,
			MaxRetries:      5,
		}

		// Cancel context after small delay
		go func() {
			time.Sleep(50 * time.Millisecond)
			cancel()
		}()

		err := utils.WithRetry(ctx, operation, opts)

		require.Error(t, err)
		require.ErrorIs(t, err, context.Canceled)
		assert.Less(t, calls, 5) // Should not have completed all retries
	})
}

func TestWithRetrySplitBatch(t *testing.T) {
	t.Parallel()

	logger := zaptest.NewLogger(t)

	tests := []struct {
		name          string
		items         []int
		batchSize     int
		minBatchSize  int
		operation     func([]int) error
		expectedCalls int
		expectedErr   error
	}{
		{
			name:         "succeeds first try",
			items:        []int{1, 2, 3, 4},
			batchSize:    4,
			minBatchSize: 1,
			operation: func(_ []int) error {
				return nil
			},
			expectedCalls: 1,
			expectedErr:   nil,
		},
		{
			name:         "splits on content block",
			items:        []int{1, 2, 3, 4},
			batchSize:    4,
			minBatchSize: 1,
			operation: func() func([]int) error {
				calls := 0
				return func(batch []int) error {
					calls++
					switch {
					case len(batch) == 4:
						// Full batch fails with content block
						return utils.ErrContentBlocked
					case len(batch) == 2 && batch[0] == 1:
						// First half fails with content block
						return utils.ErrContentBlocked
					case len(batch) == 1 && batch[0] == 1:
						// First item fails with content block
						return utils.ErrContentBlocked
					case len(batch) == 1 && batch[0] == 2:
						// Second item succeeds
						return nil
					default:
						return errOther
					}
				}
			}(),
			expectedCalls: 5, // Full batch + first half + first quarter + items 1 and 2
			expectedErr:   nil,
		},
		{
			name:         "stops at min batch size",
			items:        []int{1, 2, 3, 4},
			batchSize:    4,
			minBatchSize: 2,
			operation: func() func([]int) error {
				calls := 0
				return func(batch []int) error {
					calls++
					switch {
					case len(batch) == 4:
						// Full batch fails with content block
						return utils.ErrContentBlocked
					case len(batch) == 2:
						// At min batch size, return non-content error
						return errMinBatchSize
					default:
						return nil
					}
				}
			}(),
			expectedCalls: 3, // Full batch + both halves at min batch size
			expectedErr:   errMinBatchSize,
		},
		{
			name:         "both halves content blocked",
			items:        []int{1, 2, 3, 4},
			batchSize:    4,
			minBatchSize: 2,
			operation: func(_ []int) error {
				return utils.ErrContentBlocked
			},
			expectedCalls: 3, // Full batch + both halves
			expectedErr:   utils.ErrContentBlocked,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()
			calls := 0
			wrappedOp := func(batch []int) error {
				calls++
				return tt.operation(batch)
			}

			opts := utils.RetryOptions{
				MaxElapsedTime:  100 * time.Millisecond,
				InitialInterval: 10 * time.Millisecond,
				MaxInterval:     20 * time.Millisecond,
				MaxRetries:      3,
			}

			err := utils.WithRetrySplitBatch(ctx, tt.items, tt.batchSize, tt.minBatchSize, wrappedOp, opts, logger)

			if tt.expectedErr != nil {
				require.Error(t, err)
				require.ErrorIs(t, err, tt.expectedErr)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tt.expectedCalls, calls)
		})
	}
}

package utils_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/robalyx/rotector/pkg/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	tests := []struct {
		name              string
		items             []int
		batchSize         int
		minBatchSize      int
		operation         func([]int) error
		expectedCalls     int
		expectedErr       error
		expectedBlocked   [][]int
		expectedOnBlocked int
	}{
		{
			name:         "succeeds first try",
			items:        []int{1, 2, 3, 4},
			batchSize:    4,
			minBatchSize: 1,
			operation: func(_ []int) error {
				return nil
			},
			expectedCalls:     1,
			expectedErr:       nil,
			expectedBlocked:   nil,
			expectedOnBlocked: 0,
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
			expectedCalls:     5, // Full batch + first half + first quarter + items 1 and 2
			expectedErr:       nil,
			expectedBlocked:   [][]int{{1}},
			expectedOnBlocked: 1,
		},
		{
			name:         "stops at min batch size - all items fail with non-blocking error",
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
					case len(batch) == 1:
						// Individual items at min batch size fail with non-content error
						return errMinBatchSize
					default:
						return nil
					}
				}
			}(),
			expectedCalls:     5, // Full batch + 4 individual calls (no batch calls at minBatchSize)
			expectedErr:       errMinBatchSize,
			expectedBlocked:   nil,
			expectedOnBlocked: 0,
		},
		{
			name:         "both halves content blocked - individual processing",
			items:        []int{1, 2, 3, 4},
			batchSize:    4,
			minBatchSize: 2,
			operation: func(_ []int) error {
				return utils.ErrContentBlocked
			},
			expectedCalls:     5, // Full batch + 4 individual calls (no batch calls at minBatchSize)
			expectedErr:       utils.ErrContentBlocked,
			expectedBlocked:   [][]int{{1, 2, 3, 4}},
			expectedOnBlocked: 1,
		},
		{
			name:         "mixed good/bad items - partial success",
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
					case len(batch) == 1 && (batch[0] == 1 || batch[0] == 3):
						// Items 1 and 3 are blocked
						return utils.ErrContentBlocked
					case len(batch) == 1:
						// Items 2 and 4 succeed
						return nil
					default:
						// Batch processing blocked, split further
						return utils.ErrContentBlocked
					}
				}
			}(),
			expectedCalls:     5,   // Full batch + 4 individual calls (no batch calls at minBatchSize)
			expectedErr:       nil, // Partial success
			expectedBlocked:   [][]int{{1, 3}},
			expectedOnBlocked: 1,
		},
		{
			name:         "alternating good/bad pattern",
			items:        []int{1, 2, 3, 4, 5, 6},
			batchSize:    6,
			minBatchSize: 2,
			operation: func() func([]int) error {
				calls := 0
				return func(batch []int) error {
					calls++
					switch {
					case len(batch) > 2:
						// Any batch larger than minBatchSize gets blocked to force splitting
						return utils.ErrContentBlocked
					case len(batch) == 1:
						// Individual processing: odd numbers blocked, even numbers succeed
						if batch[0]%2 == 1 {
							return utils.ErrContentBlocked
						}
						return nil
					default:
						// Min batch size still blocked to force individual processing
						return utils.ErrContentBlocked
					}
				}
			}(),
			expectedCalls:     9,   // Complex splitting pattern + individual calls
			expectedErr:       nil, // Partial success (even numbers succeed)
			expectedBlocked:   [][]int{{1, 3, 5}},
			expectedOnBlocked: 1,
		},
		{
			name:         "single good item in batch at minBatchSize",
			items:        []int{1, 2},
			batchSize:    2,
			minBatchSize: 2,
			operation: func() func([]int) error {
				calls := 0
				return func(batch []int) error {
					calls++
					switch {
					case len(batch) == 2:
						// Batch fails, force individual processing
						return utils.ErrContentBlocked
					case len(batch) == 1 && batch[0] == 1:
						// Item 1 is blocked
						return utils.ErrContentBlocked
					case len(batch) == 1 && batch[0] == 2:
						// Item 2 succeeds
						return nil
					default:
						return errOther
					}
				}
			}(),
			expectedCalls:     2,   // 2 individual calls (no batch call at minBatchSize)
			expectedErr:       nil, // Partial success
			expectedBlocked:   [][]int{{1}},
			expectedOnBlocked: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()
			calls := 0
			blockedCalls := 0

			var blockedBatches [][]int

			wrappedOp := func(batch []int) error {
				calls++
				return tt.operation(batch)
			}

			onBlocked := func(batch []int) {
				blockedCalls++

				blockedBatches = append(blockedBatches, batch)
			}

			opts := utils.RetryOptions{
				MaxElapsedTime:  100 * time.Millisecond,
				InitialInterval: 10 * time.Millisecond,
				MaxInterval:     20 * time.Millisecond,
				MaxRetries:      3,
			}

			err := utils.WithRetrySplitBatch(ctx, tt.items, tt.batchSize, tt.minBatchSize, opts, wrappedOp, onBlocked)

			if tt.expectedErr != nil {
				require.Error(t, err)
				require.ErrorIs(t, err, tt.expectedErr)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tt.expectedCalls, calls)
			assert.Equal(t, tt.expectedOnBlocked, blockedCalls)

			if tt.expectedBlocked != nil {
				assert.Equal(t, tt.expectedBlocked, blockedBatches)
			}
		})
	}
}

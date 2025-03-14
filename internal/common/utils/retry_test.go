package utils_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/robalyx/rotector/internal/common/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errTemporary = errors.New("temporary error")

func TestWithRetry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		operation     func() (string, error)
		expectedCalls int
		expectedErr   error
		expectedRes   string
	}{
		{
			name: "succeeds first try",
			operation: func() (string, error) {
				return "success", nil
			},
			expectedCalls: 1,
			expectedErr:   nil,
			expectedRes:   "success",
		},
		{
			name: "succeeds after retries",
			operation: func() func() (string, error) {
				count := 0
				return func() (string, error) {
					count++
					if count < 3 {
						return "", errTemporary
					}
					return "success after retry", nil
				}
			}(),
			expectedCalls: 3,
			expectedErr:   nil,
			expectedRes:   "success after retry",
		},
		{
			name: "fails all retries",
			operation: func() (string, error) {
				return "", errTemporary
			},
			expectedCalls: 4, // Initial + 3 retries
			expectedErr:   errTemporary,
			expectedRes:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()
			calls := 0
			wrappedOp := func() (string, error) {
				calls++
				return tt.operation()
			}

			opts := utils.RetryOptions{
				MaxElapsedTime:  100 * time.Millisecond,
				InitialInterval: 10 * time.Millisecond,
				MaxInterval:     20 * time.Millisecond,
				MaxRetries:      3,
			}

			result, err := utils.WithRetry(ctx, wrappedOp, opts)

			if tt.expectedErr != nil {
				require.Error(t, err)
				assert.Equal(t, tt.expectedErr.Error(), err.Error())
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tt.expectedRes, result)
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

		operation := func() (string, error) {
			calls++
			return "", errTemporary
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

		result, err := utils.WithRetry(ctx, operation, opts)

		require.Error(t, err)
		assert.Equal(t, context.Canceled, err)
		assert.Equal(t, "", result)
		assert.Less(t, calls, 5) // Should not have completed all retries
	})
}

package utils_test

import (
	"context"
	"testing"
	"time"

	"github.com/robalyx/rotector/pkg/utils"
	"go.uber.org/zap"
)

func TestContextSleep(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		duration       time.Duration
		cancelAfter    time.Duration
		expectedResult utils.SleepResult
	}{
		{
			name:           "sleep completes normally",
			duration:       10 * time.Millisecond,
			cancelAfter:    0, // no cancellation
			expectedResult: utils.SleepCompleted,
		},
		{
			name:           "context cancelled before sleep completes",
			duration:       100 * time.Millisecond,
			cancelAfter:    10 * time.Millisecond,
			expectedResult: utils.SleepCancelled,
		},
		{
			name:           "zero duration sleep",
			duration:       0,
			cancelAfter:    0,
			expectedResult: utils.SleepCompleted,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			if tt.cancelAfter > 0 {
				go func() {
					time.Sleep(tt.cancelAfter)
					cancel()
				}()
			}

			result := utils.ContextSleep(ctx, tt.duration)
			if result != tt.expectedResult {
				t.Errorf("ContextSleep() = %v, want %v", result, tt.expectedResult)
			}
		})
	}
}

func TestContextSleepWithLog(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		duration       time.Duration
		cancelAfter    time.Duration
		cancelMessage  string
		expectedResult utils.SleepResult
	}{
		{
			name:           "sleep completes with logging",
			duration:       10 * time.Millisecond,
			cancelAfter:    0,
			cancelMessage:  "test message",
			expectedResult: utils.SleepCompleted,
		},
		{
			name:           "context cancelled with logging",
			duration:       100 * time.Millisecond,
			cancelAfter:    10 * time.Millisecond,
			cancelMessage:  "cancelled message",
			expectedResult: utils.SleepCancelled,
		},
		{
			name:           "context cancelled with empty message",
			duration:       100 * time.Millisecond,
			cancelAfter:    10 * time.Millisecond,
			cancelMessage:  "",
			expectedResult: utils.SleepCancelled,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			logger := zap.NewNop() // Use no-op logger for tests

			if tt.cancelAfter > 0 {
				go func() {
					time.Sleep(tt.cancelAfter)
					cancel()
				}()
			}

			result := utils.ContextSleepWithLog(ctx, tt.duration, logger, tt.cancelMessage)
			if result != tt.expectedResult {
				t.Errorf("ContextSleepWithLog() = %v, want %v", result, tt.expectedResult)
			}
		})
	}
}

func TestContextSleepUntil(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		targetOffset   time.Duration
		cancelAfter    time.Duration
		expectedResult utils.SleepResult
	}{
		{
			name:           "sleep until future time",
			targetOffset:   50 * time.Millisecond,
			cancelAfter:    0,
			expectedResult: utils.SleepCompleted,
		},
		{
			name:           "context cancelled before target time",
			targetOffset:   100 * time.Millisecond,
			cancelAfter:    10 * time.Millisecond,
			expectedResult: utils.SleepCancelled,
		},
		{
			name:           "target time in the past",
			targetOffset:   -10 * time.Millisecond,
			cancelAfter:    0,
			expectedResult: utils.SleepCompleted,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			target := time.Now().Add(tt.targetOffset)

			if tt.cancelAfter > 0 {
				go func() {
					time.Sleep(tt.cancelAfter)
					cancel()
				}()
			}

			result := utils.ContextSleepUntil(ctx, target)
			if result != tt.expectedResult {
				t.Errorf("ContextSleepUntil() = %v, want %v", result, tt.expectedResult)
			}
		})
	}
}

func TestContextSleepUntilWithLog(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		targetOffset   time.Duration
		cancelAfter    time.Duration
		cancelMessage  string
		expectedResult utils.SleepResult
	}{
		{
			name:           "sleep until with logging completes",
			targetOffset:   50 * time.Millisecond,
			cancelAfter:    0,
			cancelMessage:  "test message",
			expectedResult: utils.SleepCompleted,
		},
		{
			name:           "context cancelled with logging",
			targetOffset:   100 * time.Millisecond,
			cancelAfter:    10 * time.Millisecond,
			cancelMessage:  "cancelled message",
			expectedResult: utils.SleepCancelled,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			logger := zap.NewNop()
			target := time.Now().Add(tt.targetOffset)

			if tt.cancelAfter > 0 {
				go func() {
					time.Sleep(tt.cancelAfter)
					cancel()
				}()
			}

			result := utils.ContextSleepUntilWithLog(ctx, target, logger, tt.cancelMessage)
			if result != tt.expectedResult {
				t.Errorf("ContextSleepUntilWithLog() = %v, want %v", result, tt.expectedResult)
			}
		})
	}
}

func TestContextGuard(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		cancelContext  bool
		expectedResult bool
	}{
		{
			name:           "context not cancelled",
			cancelContext:  false,
			expectedResult: false,
		},
		{
			name:           "context cancelled",
			cancelContext:  true,
			expectedResult: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			if tt.cancelContext {
				cancel()
			}

			result := utils.ContextGuard(ctx)
			if result != tt.expectedResult {
				t.Errorf("ContextGuard() = %v, want %v", result, tt.expectedResult)
			}
		})
	}
}

func TestContextGuardWithLog(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		cancelContext  bool
		cancelMessage  string
		expectedResult bool
	}{
		{
			name:           "context not cancelled with message",
			cancelContext:  false,
			cancelMessage:  "test message",
			expectedResult: false,
		},
		{
			name:           "context cancelled with message",
			cancelContext:  true,
			cancelMessage:  "cancelled message",
			expectedResult: true,
		},
		{
			name:           "context cancelled with empty message",
			cancelContext:  true,
			cancelMessage:  "",
			expectedResult: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			logger := zap.NewNop()

			if tt.cancelContext {
				cancel()
			}

			result := utils.ContextGuardWithLog(ctx, logger, tt.cancelMessage)
			if result != tt.expectedResult {
				t.Errorf("ContextGuardWithLog() = %v, want %v", result, tt.expectedResult)
			}
		})
	}
}

func TestErrorSleep(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		duration       time.Duration
		cancelAfter    time.Duration
		workerName     string
		expectedResult bool
	}{
		{
			name:           "error sleep completes",
			duration:       10 * time.Millisecond,
			cancelAfter:    0,
			workerName:     "test worker",
			expectedResult: true,
		},
		{
			name:           "error sleep cancelled",
			duration:       100 * time.Millisecond,
			cancelAfter:    10 * time.Millisecond,
			workerName:     "test worker",
			expectedResult: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			logger := zap.NewNop()

			if tt.cancelAfter > 0 {
				go func() {
					time.Sleep(tt.cancelAfter)
					cancel()
				}()
			}

			result := utils.ErrorSleep(ctx, tt.duration, logger, tt.workerName)
			if result != tt.expectedResult {
				t.Errorf("ErrorSleep() = %v, want %v", result, tt.expectedResult)
			}
		})
	}
}

func TestThresholdSleep(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		duration       time.Duration
		cancelAfter    time.Duration
		workerName     string
		expectedResult bool
	}{
		{
			name:           "threshold sleep completes",
			duration:       10 * time.Millisecond,
			cancelAfter:    0,
			workerName:     "test worker",
			expectedResult: true,
		},
		{
			name:           "threshold sleep cancelled",
			duration:       100 * time.Millisecond,
			cancelAfter:    10 * time.Millisecond,
			workerName:     "test worker",
			expectedResult: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			logger := zap.NewNop()

			if tt.cancelAfter > 0 {
				go func() {
					time.Sleep(tt.cancelAfter)
					cancel()
				}()
			}

			result := utils.ThresholdSleep(ctx, tt.duration, logger, tt.workerName)
			if result != tt.expectedResult {
				t.Errorf("ThresholdSleep() = %v, want %v", result, tt.expectedResult)
			}
		})
	}
}

func TestIntervalSleep(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		duration       time.Duration
		cancelAfter    time.Duration
		workerName     string
		expectedResult bool
	}{
		{
			name:           "interval sleep completes",
			duration:       10 * time.Millisecond,
			cancelAfter:    0,
			workerName:     "test worker",
			expectedResult: true,
		},
		{
			name:           "interval sleep cancelled",
			duration:       100 * time.Millisecond,
			cancelAfter:    10 * time.Millisecond,
			workerName:     "test worker",
			expectedResult: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			logger := zap.NewNop()

			if tt.cancelAfter > 0 {
				go func() {
					time.Sleep(tt.cancelAfter)
					cancel()
				}()
			}

			result := utils.IntervalSleep(ctx, tt.duration, logger, tt.workerName)
			if result != tt.expectedResult {
				t.Errorf("IntervalSleep() = %v, want %v", result, tt.expectedResult)
			}
		})
	}
}

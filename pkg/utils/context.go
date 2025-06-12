package utils

import (
	"context"
	"time"

	"go.uber.org/zap"
)

// SleepResult represents the outcome of a context-aware sleep operation.
type SleepResult int

const (
	// SleepCompleted indicates the sleep duration completed normally.
	SleepCompleted SleepResult = iota
	// SleepCancelled indicates the context was cancelled during sleep.
	SleepCancelled
)

// ContextSleep sleeps for the specified duration while respecting context cancellation.
// Returns SleepCompleted if the full duration elapsed, SleepCancelled if context was cancelled.
func ContextSleep(ctx context.Context, duration time.Duration) SleepResult {
	select {
	case <-time.After(duration):
		return SleepCompleted
	case <-ctx.Done():
		return SleepCancelled
	}
}

// ContextSleepWithLog sleeps for the specified duration while respecting context cancellation,
// logging a message if the context is cancelled.
func ContextSleepWithLog(ctx context.Context, duration time.Duration, logger *zap.Logger, cancelMessage string) SleepResult {
	select {
	case <-time.After(duration):
		return SleepCompleted
	case <-ctx.Done():
		if logger != nil && cancelMessage != "" {
			logger.Info(cancelMessage)
		}
		return SleepCancelled
	}
}

// ContextSleepUntil waits until the specified time while respecting context cancellation.
// Returns SleepCompleted if the target time was reached, SleepCancelled if context was cancelled.
func ContextSleepUntil(ctx context.Context, target time.Time) SleepResult {
	duration := time.Until(target)
	if duration <= 0 {
		return SleepCompleted
	}
	return ContextSleep(ctx, duration)
}

// ContextSleepUntilWithLog waits until the specified time while respecting context cancellation,
// logging a message if the context is cancelled.
func ContextSleepUntilWithLog(ctx context.Context, target time.Time, logger *zap.Logger, cancelMessage string) SleepResult {
	duration := time.Until(target)
	if duration <= 0 {
		return SleepCompleted
	}
	return ContextSleepWithLog(ctx, duration, logger, cancelMessage)
}

// ContextGuard checks if the context is cancelled and returns true if so.
// This is useful at the beginning of loops or before starting long-running operations.
func ContextGuard(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}

// ContextGuardWithLog checks if the context is cancelled and logs a message if so.
// Returns true if context is cancelled, false otherwise.
func ContextGuardWithLog(ctx context.Context, logger *zap.Logger, cancelMessage string) bool {
	select {
	case <-ctx.Done():
		if logger != nil && cancelMessage != "" {
			logger.Info(cancelMessage)
		}
		return true
	default:
		return false
	}
}

// ErrorSleep sleeps for the specified duration when an error occurs, respecting context cancellation.
// This is a common pattern in error handling where workers need to pause before retrying.
// Returns true if should continue (sleep completed), false if should return (context cancelled).
func ErrorSleep(ctx context.Context, duration time.Duration, logger *zap.Logger, workerName string) bool {
	result := ContextSleepWithLog(ctx, duration, logger,
		"Context cancelled during error wait, stopping "+workerName)
	return result == SleepCompleted
}

// ThresholdSleep sleeps for the specified duration when a threshold is exceeded, respecting context cancellation.
// This is a common pattern when workers need to pause due to rate limits or thresholds.
// Returns true if should continue (sleep completed), false if should return (context cancelled).
func ThresholdSleep(ctx context.Context, duration time.Duration, logger *zap.Logger, workerName string) bool {
	result := ContextSleepWithLog(ctx, duration, logger,
		"Context cancelled during threshold pause, stopping "+workerName)
	return result == SleepCompleted
}

// IntervalSleep sleeps for a short interval between operations, respecting context cancellation.
// This is commonly used for brief pauses between iterations or batches.
// Returns true if should continue (sleep completed), false if should return (context cancelled).
func IntervalSleep(ctx context.Context, duration time.Duration, logger *zap.Logger, workerName string) bool {
	result := ContextSleepWithLog(ctx, duration, logger,
		"Context cancelled during pause, stopping "+workerName)
	return result == SleepCompleted
}

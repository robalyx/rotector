package dbretry

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/driver/pgdriver"
)

var (
	maxElapsedTime  = 30 * time.Second
	initialInterval = 500 * time.Millisecond
	maxInterval     = 5 * time.Second
	maxRetries      = uint64(5)
)

// IsRetryableError checks if the given error is retryable.
func IsRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Check for specific PostgreSQL error codes
	var pgerr *pgdriver.Error
	if errors.As(err, &pgerr) {
		// Retry on connection errors (class 08)
		// Retry on serialization failures (class 40)
		// Retry on deadlock detected (class 40)
		// Retry on temporary failures (class 53)
		// Retry on system errors (class 57)
		// Retry on query canceled due to conflict (class 57)
		// Retry on admin shutdown (class 57)
		// Retry on crash recovery (class 57)
		// Retry on cannot connect now (class 57)
		// Retry on database dropped during connection (class 57)
		switch pgerr.Field('C') {
		case "08000", // connection_exception
			"08003", // connection_does_not_exist
			"08006", // connection_failure
			"08001", // sqlclient_unable_to_establish_sqlconnection
			"08004", // sqlserver_rejected_establishment_of_sqlconnection
			"08007", // transaction_resolution_unknown
			"08P01", // protocol_violation
			"40001", // serialization_failure
			"40P01", // deadlock_detected
			"53000", // insufficient_resources
			"53100", // disk_full
			"53200", // out_of_memory
			"53300", // too_many_connections
			"53400", // configuration_limit_exceeded
			"57000", // operator_intervention
			"57P01", // admin_shutdown
			"57P02", // crash_shutdown
			"57P03", // cannot_connect_now
			"57P04", // database_dropped
			"55006", // object_in_use
			"55P03", // lock_not_available
			"2D000", // invalid_transaction_termination
			"3F000", // invalid_savepoint_specification
			"42P01": // undefined_table (might happen during schema migrations)
			return true
		}
	}

	// Check for network-related errors
	if errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, context.Canceled) {
		return true
	}

	// Check for common network error strings
	errMsg := err.Error()
	if strings.Contains(errMsg, "connection reset by peer") ||
		strings.Contains(errMsg, "broken pipe") ||
		strings.Contains(errMsg, "connection refused") ||
		strings.Contains(errMsg, "no connection") ||
		strings.Contains(errMsg, "i/o timeout") ||
		strings.Contains(errMsg, "EOF") {
		return true
	}

	return false
}

// Operation wraps a database operation with retry logic.
func Operation[T any](ctx context.Context, operation func(context.Context) (T, error)) (T, error) {
	var result T
	var lastErr error

	b := backoff.WithMaxRetries(backoff.NewExponentialBackOff(
		backoff.WithMaxElapsedTime(maxElapsedTime),
		backoff.WithInitialInterval(initialInterval),
		backoff.WithMaxInterval(maxInterval),
	), maxRetries)

	err := backoff.Retry(func() error {
		var err error
		result, err = operation(ctx)
		if err != nil {
			if !IsRetryableError(err) {
				// If error is not retryable, return it wrapped to stop retrying
				return backoff.Permanent(fmt.Errorf("non-retryable error: %w", err))
			}
			lastErr = err
			return err
		}
		return nil
	}, backoff.WithContext(b, ctx))
	if err != nil {
		if lastErr != nil {
			// Return the last actual database error instead of retry error
			return result, fmt.Errorf("database operation failed after retries: %w", lastErr)
		}
		return result, fmt.Errorf("database operation failed: %w", err)
	}

	return result, nil
}

// NoResult wraps a database operation that doesn't return a result.
func NoResult(ctx context.Context, operation func(context.Context) error) error {
	var lastErr error

	b := backoff.WithMaxRetries(backoff.NewExponentialBackOff(
		backoff.WithMaxElapsedTime(maxElapsedTime),
		backoff.WithInitialInterval(initialInterval),
		backoff.WithMaxInterval(maxInterval),
	), maxRetries)

	err := backoff.Retry(func() error {
		err := operation(ctx)
		if err != nil {
			if !IsRetryableError(err) {
				// If error is not retryable, return it wrapped to stop retrying
				return backoff.Permanent(fmt.Errorf("non-retryable error: %w", err))
			}
			lastErr = err
			return err
		}
		return nil
	}, backoff.WithContext(b, ctx))
	if err != nil {
		if lastErr != nil {
			// Return the last actual database error instead of retry error
			return fmt.Errorf("database operation failed after retries: %w", lastErr)
		}
		return fmt.Errorf("database operation failed: %w", err)
	}

	return nil
}

// Transaction wraps a database transaction with retry logic.
func Transaction(ctx context.Context, db *bun.DB, fn func(context.Context, bun.Tx) error) error {
	return NoResult(ctx, func(ctx context.Context) error {
		return db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
			return fn(ctx, tx)
		})
	})
}

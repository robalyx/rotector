package ai

import (
	"context"
	"errors"
	"time"

	"github.com/cenkalti/backoff/v4"
)

const (
	// ApplicationJSON is the MIME type for JSON content.
	ApplicationJSON = "application/json"
	// TextPlain is the MIME type for plain text content.
	TextPlain = "text/plain"
)

// Package-level errors.
var (
	// ErrModelResponse indicates the model returned no usable response.
	ErrModelResponse = errors.New("model response error")
	// ErrJSONProcessing indicates a JSON processing error.
	ErrJSONProcessing = errors.New("JSON processing error")
)

// withRetry executes the given operation with exponential backoff.
func withRetry[T any](ctx context.Context, operation func() (T, error)) (T, error) {
	var result T

	// Configure exponential backoff
	b := backoff.WithMaxRetries(backoff.NewExponentialBackOff(
		backoff.WithMaxElapsedTime(30*time.Second),
		backoff.WithInitialInterval(100*time.Millisecond),
		backoff.WithMaxInterval(1*time.Second),
	), 3)

	// Create backoff operation with context
	backoffOperation := func() error {
		var err error
		result, err = operation()
		return err
	}

	err := backoff.Retry(backoffOperation, backoff.WithContext(b, ctx))
	return result, err
}

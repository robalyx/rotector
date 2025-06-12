package client

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/packages/ssestream"
	"github.com/robalyx/rotector/internal/setup/config"
	"github.com/robalyx/rotector/pkg/utils"
	"github.com/sony/gobreaker"
	"go.uber.org/zap"
	"golang.org/x/sync/semaphore"
)

var ErrNoProvidersAvailable = errors.New("no providers available")

// defaultSafetySettings defines the default safety thresholds for content filtering.
var defaultSafetySettings = map[string]any{
	"safety_settings": []map[string]any{
		{"category": "HARM_CATEGORY_HARASSMENT", "threshold": "OFF"},
		{"category": "HARM_CATEGORY_HATE_SPEECH", "threshold": "OFF"},
		{"category": "HARM_CATEGORY_SEXUALLY_EXPLICIT", "threshold": "OFF"},
		{"category": "HARM_CATEGORY_DANGEROUS_CONTENT", "threshold": "OFF"},
		{"category": "HARM_CATEGORY_CIVIC_INTEGRITY", "threshold": "OFF"},
	},
}

// AIClient implements the Client interface.
type AIClient struct {
	client        *openai.Client
	breaker       *gobreaker.CircuitBreaker
	semaphore     *semaphore.Weighted
	modelMappings map[string]string
	logger        *zap.Logger
	blockChan     chan struct{}
}

// NewClient creates a new AIClient.
func NewClient(cfg *config.OpenAI, logger *zap.Logger) (*AIClient, error) {
	client := openai.NewClient(
		option.WithAPIKey(cfg.APIKey),
		option.WithBaseURL(cfg.BaseURL),
		option.WithRequestTimeout(90*time.Second),
		option.WithMaxRetries(0),
	)

	// Create circuit breaker settings
	settings := gobreaker.Settings{
		Name:        "openai",
		MaxRequests: 1,
		Timeout:     60 * time.Second,
		Interval:    0,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			failureRatio := float64(counts.TotalFailures) / float64(counts.Requests)
			return counts.Requests >= 10 && failureRatio >= 0.6
		},
		OnStateChange: func(_ string, from gobreaker.State, to gobreaker.State) {
			logger.Warn("Circuit breaker state changed",
				zap.String("from", from.String()),
				zap.String("to", to.String()))
		},
	}

	return &AIClient{
		client:        &client,
		breaker:       gobreaker.NewCircuitBreaker(settings),
		semaphore:     semaphore.NewWeighted(cfg.MaxConcurrent),
		modelMappings: cfg.ModelMappings,
		logger:        logger.Named("ai_client"),
		blockChan:     make(chan struct{}),
	}, nil
}

// Chat returns a ChatCompletions implementation.
func (c *AIClient) Chat() ChatCompletions {
	return &chatCompletions{client: c}
}

// blockIndefinitely blocks the program indefinitely when the circuit breaker opens.
func (c *AIClient) blockIndefinitely(model string, err error) {
	c.logger.Error("Circuit breaker is open - system requires immediate attention. Pausing indefinitely.",
		zap.String("model", model),
		zap.Error(err))
	<-c.blockChan // This will block forever since no one sends to this channel
}

// chatCompletions implements the ChatCompletions interface.
type chatCompletions struct {
	client *AIClient
}

// New makes a chat completion request.
func (c *chatCompletions) New(ctx context.Context, params openai.ChatCompletionNewParams) (*openai.ChatCompletion, error) {
	// Map model name
	originalModel := params.Model
	if mappedModel, ok := c.client.modelMappings[originalModel]; ok {
		params.Model = mappedModel
	} else {
		return nil, fmt.Errorf("%w: %s", ErrNoProvidersAvailable, originalModel)
	}

	// Add safety settings
	params.SetExtraFields(defaultSafetySettings)

	// Try to acquire semaphore
	if err := c.client.semaphore.Acquire(ctx, 1); err != nil {
		return nil, fmt.Errorf("failed to acquire semaphore: %w", err)
	}
	defer c.client.semaphore.Release(1)

	// Execute request
	result, err := c.client.breaker.Execute(func() (any, error) {
		resp, err := c.client.client.Chat.Completions.New(ctx, params)
		if bl := c.checkBlockReasons(resp, params.Model); bl != nil {
			return resp, bl
		}
		return resp, err
	})
	if err != nil {
		switch {
		case errors.Is(err, gobreaker.ErrOpenState):
			c.client.blockIndefinitely(params.Model, err)
			return nil, fmt.Errorf("system failure - circuit breaker is open: %w", err)
		case errors.Is(err, utils.ErrContentBlocked):
			return nil, err
		default:
			c.client.logger.Warn("Failed to make request", zap.Error(err))
			return nil, err
		}
	}

	return result.(*openai.ChatCompletion), nil
}

// NewWithRetry makes a chat completion request with retry logic.
func (c *chatCompletions) NewWithRetry(
	ctx context.Context, params openai.ChatCompletionNewParams, callback RetryCallback,
) error {
	// Map initial model name
	originalModel := params.Model
	if mappedModel, ok := c.client.modelMappings[originalModel]; ok {
		params.Model = mappedModel
	} else {
		return fmt.Errorf("%w: %s", ErrNoProvidersAvailable, originalModel)
	}

	// Add safety settings
	params.SetExtraFields(defaultSafetySettings)

	// Try to acquire semaphore
	if err := c.client.semaphore.Acquire(ctx, 1); err != nil {
		return fmt.Errorf("failed to acquire semaphore: %w", err)
	}
	defer c.client.semaphore.Release(1)

	var (
		attempt uint64
		resp    *openai.ChatCompletion
		lastErr error
	)

	options := utils.GetAIRetryOptions()

	// Create retry operation
	operation := func() error {
		// Check context before making request
		if err := ctx.Err(); err != nil {
			return backoff.Permanent(err)
		}

		attempt++

		// Execute request with circuit breaker
		result, err := c.client.breaker.Execute(func() (any, error) {
			var execErr error
			resp, execErr = c.client.client.Chat.Completions.New(ctx, params)
			if bl := c.checkBlockReasons(resp, params.Model); bl != nil {
				return resp, bl
			}
			return resp, execErr
		})
		if err != nil {
			lastErr = err
			switch {
			case errors.Is(err, gobreaker.ErrOpenState):
				c.client.blockIndefinitely(params.Model, err)
				return backoff.Permanent(fmt.Errorf("system failure - circuit breaker is open: %w", err))
			case errors.Is(err, utils.ErrContentBlocked):
				return backoff.Permanent(err)
			default:
				c.client.logger.Warn("Failed to make request",
					zap.Error(err),
					zap.String("model", params.Model),
					zap.Uint64("attempt", attempt))
			}

			// Call callback to handle response and error
			if cbErr := callback(resp, err); cbErr != nil {
				permanentError := &backoff.PermanentError{}
				if errors.As(cbErr, &permanentError) {
					return backoff.Permanent(fmt.Errorf("permanent callback error: %w", cbErr))
				}

				c.client.logger.Warn("Callback error, will retry",
					zap.Error(cbErr),
					zap.Uint64("attempt", attempt))
				return cbErr
			}

			return err
		}

		// Call callback for successful response
		resp = result.(*openai.ChatCompletion)
		if cbErr := callback(resp, nil); cbErr != nil {
			permanentError := &backoff.PermanentError{}
			if errors.As(cbErr, &permanentError) {
				return backoff.Permanent(fmt.Errorf("permanent callback error: %w", cbErr))
			}
			return cbErr
		}
		return nil
	}

	// Execute with retry
	if err := utils.WithRetry(ctx, operation, options); err != nil {
		if lastErr != nil {
			return fmt.Errorf("all retry attempts failed: %w (last error: %w)", err, lastErr)
		}
		return fmt.Errorf("all retry attempts failed: %w", err)
	}

	return nil
}

// NewStreaming creates a streaming chat completion request.
func (c *chatCompletions) NewStreaming(
	ctx context.Context, params openai.ChatCompletionNewParams,
) *ssestream.Stream[openai.ChatCompletionChunk] {
	// Map model name
	originalModel := params.Model
	if mappedModel, ok := c.client.modelMappings[originalModel]; ok {
		params.Model = mappedModel
	} else {
		return ssestream.NewStream[openai.ChatCompletionChunk](
			nil, fmt.Errorf("%w: %s", ErrNoProvidersAvailable, originalModel),
		)
	}

	// Add safety settings
	params.SetExtraFields(defaultSafetySettings)

	// Try to acquire semaphore
	if err := c.client.semaphore.Acquire(ctx, 1); err != nil {
		return ssestream.NewStream[openai.ChatCompletionChunk](
			nil, fmt.Errorf("failed to acquire semaphore: %w", err),
		)
	}

	// Execute stream creation with circuit breaker
	result, err := c.client.breaker.Execute(func() (any, error) {
		stream := c.client.client.Chat.Completions.NewStreaming(ctx, params)
		if stream.Err() != nil {
			return nil, stream.Err()
		}
		return stream, nil
	})
	if err != nil {
		c.client.semaphore.Release(1)
		if errors.Is(err, gobreaker.ErrOpenState) {
			c.client.blockIndefinitely(params.Model, err)
			return ssestream.NewStream[openai.ChatCompletionChunk](
				nil, fmt.Errorf("system failure - circuit breaker is open: %w", err))
		}
		c.client.logger.Warn("Failed to create stream", zap.Error(err))
		return ssestream.NewStream[openai.ChatCompletionChunk](nil, err)
	}

	stream := result.(*ssestream.Stream[openai.ChatCompletionChunk])

	// Set up cleanup when context is done
	go func() {
		<-ctx.Done()
		c.client.semaphore.Release(1)
	}()

	return stream
}

// checkBlockReasons checks if the response was blocked by content filtering.
func (c *chatCompletions) checkBlockReasons(resp *openai.ChatCompletion, model string) error {
	// Check if response is provided
	if resp == nil {
		c.client.logger.Warn("Received nil response", zap.String("model", model))
		return fmt.Errorf("%w: received nil response", utils.ErrContentBlocked)
	}

	// Check if choices are provided
	if len(resp.Choices) == 0 {
		c.client.logger.Warn("Received empty choices", zap.String("model", model))
		return fmt.Errorf("%w: received empty choices", utils.ErrContentBlocked)
	}

	// Check if finish reason is provided
	finishReason := resp.Choices[0].FinishReason
	if finishReason == "" {
		c.client.logger.Warn("No finish reason provided", zap.String("model", model))
		return fmt.Errorf("%w: no finish reason provided", utils.ErrContentBlocked)
	}

	// Map of finish reasons to their error handling
	finishReasonHandlers := map[string]error{
		"content_filter": utils.ErrContentBlocked,
		"stop":           nil,
	}

	err, known := finishReasonHandlers[finishReason]
	if !known {
		c.client.logger.Warn("Unknown finish reason",
			zap.String("model", model),
			zap.String("finishReason", finishReason))
		err = utils.ErrContentBlocked
	}

	if err != nil {
		c.client.logger.Warn("Content blocked",
			zap.String("model", model),
			zap.String("finishReason", finishReason))
		return backoff.Permanent(err)
	}

	return nil
}

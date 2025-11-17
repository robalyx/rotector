package client

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/packages/ssestream"
	"github.com/robalyx/rotector/internal/cloudflare/manager"
	"github.com/robalyx/rotector/internal/setup/config"
	"github.com/robalyx/rotector/pkg/utils"
	"github.com/sony/gobreaker"
	"go.uber.org/zap"
	"golang.org/x/sync/semaphore"
)

var (
	ErrNoProvidersAvailable = errors.New("no providers available")
	ErrInvalidResponse      = errors.New("invalid response from API")
	ErrResponseTruncated    = errors.New("response truncated at max_tokens limit")
)

// geminiSafetySettings defines safety thresholds for Gemini models.
var geminiSafetySettings = []map[string]any{
	{"category": "HARM_CATEGORY_HARASSMENT", "threshold": "OFF"},
	{"category": "HARM_CATEGORY_HATE_SPEECH", "threshold": "OFF"},
	{"category": "HARM_CATEGORY_SEXUALLY_EXPLICIT", "threshold": "OFF"},
	{"category": "HARM_CATEGORY_DANGEROUS_CONTENT", "threshold": "OFF"},
	{"category": "HARM_CATEGORY_CIVIC_INTEGRITY", "threshold": "OFF"},
}

// AIClient implements the Client interface.
type AIClient struct {
	client        *openai.Client
	breaker       *gobreaker.CircuitBreaker
	semaphore     *semaphore.Weighted
	modelMappings map[string]string
	modelPricing  map[string]config.ModelPricing
	usageTracker  *manager.AIUsage
	logger        *zap.Logger
	blockChan     chan struct{}
}

// NewClient creates a new AIClient.
func NewClient(cfg *config.OpenAI, usageTracker *manager.AIUsage, logger *zap.Logger) (*AIClient, error) {
	// Create OpenAI client
	credentials := cfg.Username + ":" + cfg.Password
	encodedCredentials := base64.StdEncoding.EncodeToString([]byte(credentials))
	authHeader := "Basic " + encodedCredentials

	client := openai.NewClient(
		option.WithHeader("Authorization", authHeader),
		option.WithBaseURL(cfg.BaseURL),
		option.WithRequestTimeout(60*time.Second),
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
		modelPricing:  cfg.ModelPricing,
		usageTracker:  usageTracker,
		logger:        logger.Named("ai_client"),
		blockChan:     make(chan struct{}),
	}, nil
}

// Chat returns a ChatCompletions implementation.
func (c *AIClient) Chat() ChatCompletions {
	return &chatCompletions{client: c}
}

// blockIndefinitely blocks the program indefinitely when the circuit breaker opens.
func (c *AIClient) blockIndefinitely(ctx context.Context, model string, err error) {
	c.logger.Error("Circuit breaker is open - system requires immediate attention. Pausing indefinitely.",
		zap.String("model", model),
		zap.Error(err))

	select {
	case <-c.blockChan: // This will block forever since no one sends to this channel
		c.logger.Info("Circuit breaker block released")
	case <-ctx.Done():
		c.logger.Info("Graceful shutdown requested while circuit breaker was open",
			zap.String("model", model),
			zap.Error(ctx.Err()))
	}
}

// trackUsage records AI usage statistics to the D1 database.
func (c *AIClient) trackUsage(ctx context.Context, modelName string, usage openai.CompletionUsage) {
	// Look up pricing for this model
	pricing, ok := c.modelPricing[modelName]
	if !ok {
		c.logger.Warn("No pricing configured for model, skipping usage tracking",
			zap.String("model", modelName))

		return
	}

	// Extract token counts
	promptTokens := usage.PromptTokens
	completionTokens := usage.CompletionTokens
	reasoningTokens := usage.CompletionTokensDetails.ReasoningTokens

	// Calculate cost (pricing is per million tokens)
	cost := (float64(promptTokens)*pricing.Input +
		float64(completionTokens)*pricing.Completion +
		float64(reasoningTokens)*pricing.Reasoning) / 1_000_000

	// Get today's date in UTC formatted as YYYY-MM-DD
	date := time.Now().UTC().Format("2006-01-02")

	// Update daily usage in D1
	if err := c.usageTracker.UpdateDailyUsage(ctx, date, promptTokens, completionTokens, reasoningTokens, cost); err != nil {
		c.logger.Warn("Failed to update AI usage tracking",
			zap.Error(err),
			zap.String("model", modelName),
			zap.String("date", date))
	}
}

// applyModelSettings applies model settings such as Gemini safety settings.
func (c *AIClient) applyModelSettings(params *openai.ChatCompletionNewParams) {
	if !strings.Contains(strings.ToLower(params.Model), "gemini") {
		return
	}

	extraFields := map[string]any{
		"safety_settings": geminiSafetySettings,
		"providerOptions": map[string]any{
			"gateway": map[string]any{
				"only": []string{"vertex"},
			},
		},
	}

	params.SetExtraFields(extraFields)
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

	c.client.applyModelSettings(&params)

	// Try to acquire semaphore
	if err := c.client.semaphore.Acquire(ctx, 1); err != nil {
		return nil, fmt.Errorf("failed to acquire semaphore: %w", err)
	}
	defer c.client.semaphore.Release(1)

	// Execute request
	result, err := c.client.breaker.Execute(func() (any, error) {
		resp, err := c.client.client.Chat.Completions.New(ctx, params)
		if err != nil {
			return resp, err
		}

		if bl := c.checkBlockReasons(resp, params.Model); bl != nil {
			return resp, bl
		}

		return resp, nil
	})
	if err != nil {
		switch {
		case errors.Is(err, gobreaker.ErrOpenState):
			c.client.blockIndefinitely(ctx, params.Model, err)
			return nil, fmt.Errorf("system failure - circuit breaker is open: %w", err)
		case errors.Is(err, utils.ErrContentBlocked):
			return nil, err
		default:
			c.client.logger.Warn("Failed to make request", zap.Error(err))
			return nil, err
		}
	}

	resp := result.(*openai.ChatCompletion)

	c.client.trackUsage(ctx, originalModel, resp.Usage)

	return resp, nil
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

	c.client.applyModelSettings(&params)

	// Try to acquire semaphore
	if err := c.client.semaphore.Acquire(ctx, 1); err != nil {
		return fmt.Errorf("failed to acquire semaphore: %w", err)
	}
	defer c.client.semaphore.Release(1)

	var (
		attempt              uint64
		resp                 *openai.ChatCompletion
		lastErr              error
		triedWithoutThinking bool
	)

	options := utils.GetAIRetryOptions()

	// Create retry operation
	operation := func() error {
		// Check context before making request
		if err := ctx.Err(); err != nil {
			return backoff.Permanent(err)
		}

		// Auto-disable thinking mode on truncation
		if errors.Is(lastErr, ErrResponseTruncated) {
			if !triedWithoutThinking {
				if extraFields := params.ExtraFields(); extraFields != nil {
					if reasoning, ok := extraFields["reasoning"].(map[string]any); ok {
						if enabled, ok := reasoning["enabled"].(bool); ok && enabled {
							reasoning["enabled"] = false
							triedWithoutThinking = true
							lastErr = nil

							c.client.logger.Info("Response truncated, disabling thinking mode for retry",
								zap.String("model", params.Model),
								zap.Uint64("attempt", attempt))
						}
					}
				}
			} else {
				c.client.logger.Warn("Response still truncated after disabling thinking mode",
					zap.String("model", params.Model))

				return backoff.Permanent(lastErr)
			}
		}

		attempt++

		// Execute request with circuit breaker
		result, err := c.client.breaker.Execute(func() (any, error) {
			var execErr error

			resp, execErr = c.client.client.Chat.Completions.New(ctx, params)
			if execErr != nil {
				return resp, execErr
			}

			if bl := c.checkBlockReasons(resp, params.Model); bl != nil {
				return resp, bl
			}

			return resp, nil
		})
		if err != nil {
			lastErr = err
			switch {
			case errors.Is(err, gobreaker.ErrOpenState):
				c.client.blockIndefinitely(ctx, params.Model, err)
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

		c.client.trackUsage(ctx, originalModel, resp.Usage)

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

// NewWithRetryAndFallback makes a chat completion request with retry logic and fallback model support.
func (c *chatCompletions) NewWithRetryAndFallback(
	ctx context.Context, params openai.ChatCompletionNewParams, fallbackModel string, callback RetryCallback,
) error {
	originalModel := params.Model

	// Try primary model first
	err := c.NewWithRetry(ctx, params, callback)

	// If content blocked or no provider mapping and fallback configured, try fallback
	if (errors.Is(err, utils.ErrContentBlocked) || errors.Is(err, ErrNoProvidersAvailable)) && fallbackModel != "" {
		c.client.logger.Warn("Content blocked or no provider available, attempting fallback model",
			zap.String("original_model", originalModel),
			zap.String("fallback_model", fallbackModel))

		// Update params with fallback model
		params.Model = fallbackModel

		// Retry with fallback model
		if fallbackErr := c.NewWithRetry(ctx, params, callback); fallbackErr != nil {
			c.client.logger.Error("Fallback model also failed",
				zap.String("fallback_model", fallbackModel),
				zap.Error(fallbackErr))

			return fmt.Errorf("both primary and fallback failed: primary=%w, fallback=%w", err, fallbackErr)
		}

		c.client.logger.Info("Fallback model succeeded",
			zap.String("original_model", originalModel),
			zap.String("fallback_model", fallbackModel))

		return nil
	}

	return err
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

	c.client.applyModelSettings(&params)

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
			c.client.blockIndefinitely(ctx, params.Model, err)

			return ssestream.NewStream[openai.ChatCompletionChunk](
				nil, fmt.Errorf("system failure - circuit breaker is open: %w", err))
		}

		c.client.logger.Warn("Failed to create stream", zap.Error(err))

		return ssestream.NewStream[openai.ChatCompletionChunk](nil, err)
	}

	stream := result.(*ssestream.Stream[openai.ChatCompletionChunk])

	// Release semaphore when context is done
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
		return fmt.Errorf("%w: received nil response", ErrInvalidResponse)
	}

	// Check if choices are provided
	if len(resp.Choices) == 0 {
		c.client.logger.Warn("Received empty choices", zap.String("model", model))
		return fmt.Errorf("%w: received empty choices", ErrInvalidResponse)
	}

	// Check if finish reason is provided
	finishReason := resp.Choices[0].FinishReason
	if finishReason == "" {
		c.client.logger.Warn("No finish reason provided", zap.String("model", model))
		return fmt.Errorf("%w: no finish reason provided", ErrInvalidResponse)
	}

	// Map of finish reasons to their error handling
	finishReasonHandlers := map[string]error{
		"content_filter": utils.ErrContentBlocked,
		"stop":           nil,
		"length":         nil,
	}

	err, known := finishReasonHandlers[finishReason]
	if !known {
		c.client.logger.Warn("Unknown finish reason",
			zap.String("model", model),
			zap.String("finishReason", finishReason))

		return fmt.Errorf("%w: unknown finish reason: %s", ErrInvalidResponse, finishReason)
	}

	// Check if response was truncated due to length limit
	if finishReason == "length" {
		c.client.logger.Warn("Response truncated at max_tokens limit",
			zap.String("model", model))

		return ErrResponseTruncated
	}

	if err != nil {
		c.client.logger.Warn("Content blocked",
			zap.String("model", model),
			zap.String("finishReason", finishReason))

		return backoff.Permanent(err)
	}

	return nil
}

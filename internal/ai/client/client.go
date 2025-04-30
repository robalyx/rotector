package client

import (
	"context"
	"errors"
	"fmt"
	"time"

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

// WithReasoning adds reasoning fields to the chat completion parameters.
func WithReasoning(params openai.ChatCompletionNewParams, opts ReasoningOptions) openai.ChatCompletionNewParams {
	params.WithExtraFields(map[string]any{
		"reasoning": map[string]any{
			"effort":     string(opts.Effort),
			"max_tokens": opts.MaxTokens,
			"exclude":    opts.Exclude,
		},
	})
	return params
}

// AIClient implements the Client interface.
type AIClient struct {
	providers []*providerClient
	config    *config.OpenAI
	logger    *zap.Logger
}

// NewClient creates a new AIClient.
func NewClient(cfg *config.OpenAI, logger *zap.Logger) (*AIClient, error) {
	providers := make([]*providerClient, len(cfg.Providers))

	for i, p := range cfg.Providers {
		client := openai.NewClient(
			option.WithAPIKey(p.APIKey),
			option.WithBaseURL(p.BaseURL),
			option.WithRequestTimeout(90*time.Second),
			option.WithMaxRetries(0),
		)

		// Create circuit breaker settings
		settings := gobreaker.Settings{
			Name:        p.Name,
			MaxRequests: 1,
			Timeout:     60 * time.Second,
			Interval:    0,
			ReadyToTrip: func(counts gobreaker.Counts) bool {
				failureRatio := float64(counts.TotalFailures) / float64(counts.Requests)
				return counts.Requests >= 5 && failureRatio >= 0.6
			},
			OnStateChange: func(name string, from gobreaker.State, to gobreaker.State) {
				logger.Warn("Circuit breaker state changed",
					zap.String("provider", name),
					zap.String("from", from.String()),
					zap.String("to", to.String()))
			},
		}

		providers[i] = &providerClient{
			client:        &client,
			breaker:       gobreaker.NewCircuitBreaker(settings),
			semaphore:     semaphore.NewWeighted(p.MaxConcurrent),
			modelMappings: p.ModelMappings,
			name:          p.Name,
		}
	}

	return &AIClient{
		providers: providers,
		config:    cfg,
		logger:    logger.Named("ai_client"),
	}, nil
}

// Chat returns a ChatCompletions implementation.
func (c *AIClient) Chat() ChatCompletions {
	return &chatCompletions{client: c}
}

// chatCompletions implements the ChatCompletions interface.
type chatCompletions struct {
	client *AIClient
}

// New makes a chat completion request to an available provider.
func (c *chatCompletions) New(ctx context.Context, params openai.ChatCompletionNewParams) (*openai.ChatCompletion, error) {
	// Get available providers that support this model
	availableProviders := make([]*providerClient, 0)
	originalModel := params.Model

	for _, provider := range c.client.providers {
		if _, ok := provider.modelMappings[originalModel]; ok {
			availableProviders = append(availableProviders, provider)
		}
	}

	if len(availableProviders) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrNoProvidersAvailable, originalModel)
	}

	// Try to find available provider
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
			var selectedProvider *providerClient

			// Try to acquire from any provider
			for _, provider := range availableProviders {
				if provider.semaphore.TryAcquire(1) {
					selectedProvider = provider
					break
				}
			}

			if selectedProvider != nil {
				// Prepare parameters
				newParams := c.prepareParams(params, selectedProvider)

				// Execute request
				result, err := selectedProvider.breaker.Execute(func() (any, error) {
					resp, err := selectedProvider.client.Chat.Completions.New(ctx, newParams)
					if bl := c.checkBlockReasons(resp, selectedProvider, newParams.Model); bl != nil {
						return resp, bl
					}
					return resp, err
				})
				selectedProvider.semaphore.Release(1)

				if err != nil {
					switch {
					case errors.Is(err, gobreaker.ErrOpenState):
						c.client.logger.Warn("Circuit breaker is open",
							zap.String("provider", selectedProvider.name))
					case errors.Is(err, utils.ErrContentBlocked):
						return nil, err
					default:
						c.client.logger.Warn("Failed to make request",
							zap.String("provider", selectedProvider.name),
							zap.Error(err))
					}
					time.Sleep(1 * time.Second)
					continue
				}

				return result.(*openai.ChatCompletion), nil
			}

			// All providers at capacity, wait briefly before retry
			time.Sleep(100 * time.Millisecond)
		}
	}
}

// NewStreaming creates a streaming chat completion request to an available provider.
func (c *chatCompletions) NewStreaming(
	ctx context.Context, params openai.ChatCompletionNewParams,
) *ssestream.Stream[openai.ChatCompletionChunk] {
	// Get available providers that support this model
	availableProviders := make([]*providerClient, 0)
	originalModel := params.Model

	for _, provider := range c.client.providers {
		if _, ok := provider.modelMappings[originalModel]; ok {
			availableProviders = append(availableProviders, provider)
		}
	}

	if len(availableProviders) == 0 {
		c.client.logger.Fatal("No providers available for streaming with model",
			zap.String("model", originalModel))
		return ssestream.NewStream[openai.ChatCompletionChunk](
			nil, fmt.Errorf("%w: %s", ErrNoProvidersAvailable, originalModel),
		)
	}

	// Try to find available provider
	for {
		select {
		case <-ctx.Done():
			return ssestream.NewStream[openai.ChatCompletionChunk](nil, ctx.Err())
		default:
			var selectedProvider *providerClient

			// Try to acquire from any provider
			for _, provider := range availableProviders {
				if provider.semaphore.TryAcquire(1) {
					selectedProvider = provider
					break
				}
			}

			if selectedProvider != nil {
				// Prepare parameters
				newParams := c.prepareParams(params, selectedProvider)

				// Execute stream creation
				result, err := selectedProvider.breaker.Execute(func() (any, error) {
					stream := selectedProvider.client.Chat.Completions.NewStreaming(ctx, newParams)
					if stream.Err() != nil {
						return nil, stream.Err()
					}
					return stream, nil
				})
				if err != nil {
					selectedProvider.semaphore.Release(1)
					if errors.Is(err, gobreaker.ErrOpenState) {
						c.client.logger.Warn("Circuit breaker is open for streaming",
							zap.String("provider", selectedProvider.name))
					} else {
						c.client.logger.Warn("Failed to create stream",
							zap.String("provider", selectedProvider.name),
							zap.Error(err))
					}
					time.Sleep(1 * time.Second)
					continue
				}

				stream := result.(*ssestream.Stream[openai.ChatCompletionChunk])

				// Set up cleanup when context is done
				go func() {
					<-ctx.Done()
					selectedProvider.semaphore.Release(1)
				}()

				return stream
			}

			// All providers at capacity, wait briefly before retry
			time.Sleep(100 * time.Millisecond)
		}
	}
}

// checkBlockReasons checks if the response was blocked by content filtering.
func (c *chatCompletions) checkBlockReasons(resp *openai.ChatCompletion, provider *providerClient, model string) error {
	if len(resp.Choices) == 0 {
		return nil
	}

	finishReason := resp.Choices[0].FinishReason
	switch finishReason {
	case "content_filter":
		c.client.logger.Warn("Content blocked by provider",
			zap.String("provider", provider.name),
			zap.String("model", model),
			zap.String("finishReason", finishReason))
		return utils.ErrContentBlocked
	case "stop":
		return nil
	case "":
		c.client.logger.Warn("No finish reason",
			zap.String("provider", provider.name),
			zap.String("model", model))
		return nil
	default:
		c.client.logger.Warn("Unknown finish reason",
			zap.String("provider", provider.name),
			zap.String("model", model),
			zap.String("finishReason", finishReason))
		return utils.ErrContentBlocked
	}
}

// prepareParams prepares the chat completion parameters by mapping the model name and adding safety settings.
func (c *chatCompletions) prepareParams(params openai.ChatCompletionNewParams, provider *providerClient) openai.ChatCompletionNewParams {
	originalModel := params.Model
	params.Model = provider.modelMappings[originalModel]

	params.WithExtraFields(map[string]any{
		"safety_settings": []map[string]any{
			{"category": "HARM_CATEGORY_HARASSMENT", "threshold": "OFF"},
			{"category": "HARM_CATEGORY_HATE_SPEECH", "threshold": "OFF"},
			{"category": "HARM_CATEGORY_SEXUALLY_EXPLICIT", "threshold": "OFF"},
			{"category": "HARM_CATEGORY_DANGEROUS_CONTENT", "threshold": "OFF"},
			{"category": "HARM_CATEGORY_CIVIC_INTEGRITY", "threshold": "OFF"},
		},
	})

	c.client.logger.Debug("Using provider",
		zap.String("provider", provider.name),
		zap.String("originalModel", originalModel),
		zap.String("mappedModel", params.Model))

	return params
}

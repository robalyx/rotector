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
	"github.com/sony/gobreaker"
	"go.uber.org/zap"
	"golang.org/x/sync/semaphore"
)

var ErrNoProvidersAvailable = errors.New("no providers available")

// CircuitBreakerSettings contains configuration for the circuit breaker.
type CircuitBreakerSettings struct {
	MaxFailures     uint32
	Timeout         time.Duration
	HalfOpenMaxReqs uint32
}

// Client provides a unified interface for making AI requests.
type Client interface {
	Chat() ChatCompletions
}

// ChatCompletions provides chat completion methods.
type ChatCompletions interface {
	New(ctx context.Context, params openai.ChatCompletionNewParams) (*openai.ChatCompletion, error)
	NewStreaming(ctx context.Context, params openai.ChatCompletionNewParams) *ssestream.Stream[openai.ChatCompletionChunk]
}

// providerClient represents a single provider's client and configuration.
type providerClient struct {
	client        *openai.Client
	breaker       *gobreaker.CircuitBreaker
	semaphore     *semaphore.Weighted
	modelMappings map[string]string
	name          string
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
			option.WithRequestTimeout(30*time.Second),
			option.WithMaxRetries(0),
		)

		// Create circuit breaker settings
		settings := gobreaker.Settings{
			Name:        p.Name,
			MaxRequests: 1,
			Timeout:     30 * time.Second,
			Interval:    0,
			ReadyToTrip: func(counts gobreaker.Counts) bool {
				failureRatio := float64(counts.TotalFailures) / float64(counts.Requests)
				return counts.Requests >= 3 && failureRatio >= 0.6
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
				// Map model name
				params.Model = selectedProvider.modelMappings[originalModel]

				c.client.logger.Debug("Using provider for request",
					zap.String("provider", selectedProvider.name),
					zap.String("originalModel", originalModel),
					zap.String("mappedModel", params.Model))

				// Execute request
				result, err := selectedProvider.breaker.Execute(func() (interface{}, error) {
					return selectedProvider.client.Chat.Completions.New(ctx, params)
				})

				selectedProvider.semaphore.Release(1)

				if err != nil {
					if errors.Is(err, gobreaker.ErrOpenState) {
						c.client.logger.Warn("Circuit breaker is open",
							zap.String("provider", selectedProvider.name))
						continue
					}
					c.client.logger.Warn("Failed to make request",
						zap.String("provider", selectedProvider.name),
						zap.Error(err))
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
		return nil
	}

	// Try to find available provider
	for {
		select {
		case <-ctx.Done():
			return nil
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
				// Map model name
				params.Model = selectedProvider.modelMappings[originalModel]

				c.client.logger.Debug("Using provider for streaming",
					zap.String("provider", selectedProvider.name),
					zap.String("originalModel", originalModel),
					zap.String("mappedModel", params.Model))

				// Execute stream creation
				result, err := selectedProvider.breaker.Execute(func() (any, error) {
					stream := selectedProvider.client.Chat.Completions.NewStreaming(ctx, params)
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
						continue
					}
					c.client.logger.Warn("Failed to create stream",
						zap.String("provider", selectedProvider.name),
						zap.Error(err))
					continue
				}

				stream := result.(*ssestream.Stream[openai.ChatCompletionChunk])

				// Set up cleanup when stream ends
				go func() {
					for stream.Next() {
						// Wait for stream to complete
					}
					selectedProvider.semaphore.Release(1)
				}()

				return stream
			}

			// All providers at capacity, wait briefly before retry
			time.Sleep(100 * time.Millisecond)
		}
	}
}

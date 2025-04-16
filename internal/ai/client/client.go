package client

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/packages/ssestream"
	"github.com/robalyx/rotector/internal/setup/config"
	"github.com/sony/gobreaker"
	"go.uber.org/zap"
	"golang.org/x/sync/semaphore"
)

var (
	ErrNoProvidersAvailable = errors.New("no providers available")
	ErrContentBlocked       = errors.New("content blocked by provider")
)

// BlockReason represents different types of content blocking reasons.
type BlockReason string

const (
	BlockReasonUnspecified BlockReason = "BLOCK_REASON_UNSPECIFIED"
	BlockReasonSafety      BlockReason = "SAFETY"
	BlockReasonOther       BlockReason = "OTHER"
	BlockReasonBlocklist   BlockReason = "BLOCKLIST"
	BlockReasonProhibited  BlockReason = "PROHIBITED_CONTENT"
	BlockReasonImageSafety BlockReason = "IMAGE_SAFETY"
)

// String returns the string representation of the block reason.
func (br BlockReason) String() string {
	return string(br)
}

// Details returns human-readable details about the block reason.
func (br BlockReason) Details() string {
	switch br {
	case BlockReasonSafety:
		return "Check safetyRatings for specific category"
	case BlockReasonBlocklist:
		return "Content matched terminology blocklist"
	case BlockReasonImageSafety:
		return "Unsafe image generation content"
	case BlockReasonProhibited:
		return "Content violates provider's content policy"
	case BlockReasonOther:
		return "Content blocked for unspecified reasons"
	case BlockReasonUnspecified:
		return "Default block reason (unused)"
	}
	return "Unknown block reason"
}

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

// checkBlockReasons checks the response for various block reasons and logs them appropriately.
func (c *chatCompletions) checkBlockReasons(resp *openai.ChatCompletion, provider *providerClient, model string) error {
	rawJSON := resp.RawJSON()
	blockReasons := []BlockReason{
		BlockReasonUnspecified,
		BlockReasonSafety,
		BlockReasonOther,
		BlockReasonBlocklist,
		BlockReasonProhibited,
		BlockReasonImageSafety,
	}

	for _, reason := range blockReasons {
		if strings.Contains(rawJSON, fmt.Sprintf(`"native_finish_reason":"%s"`, reason)) {
			fields := []zap.Field{
				zap.String("provider", provider.name),
				zap.String("model", model),
				zap.String("blockReason", reason.String()),
				zap.String("details", reason.Details()),
			}

			// Add raw JSON for debugging, but only for non-default cases
			if reason != BlockReasonUnspecified {
				fields = append(fields, zap.String("rawJSON", rawJSON))
			}

			c.client.logger.Warn("Content blocked by provider", fields...)
			return fmt.Errorf("%w: %s", ErrContentBlocked, reason.Details())
		}
	}

	return nil
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
				params = c.prepareParams(params, selectedProvider)

				// Execute request
				result, err := selectedProvider.breaker.Execute(func() (any, error) {
					resp, err := selectedProvider.client.Chat.Completions.New(ctx, params)
					if err != nil {
						return resp, err
					}
					if err := c.checkBlockReasons(resp, selectedProvider, params.Model); err != nil {
						return resp, err
					}
					return resp, nil
				})
				selectedProvider.semaphore.Release(1)

				if err != nil {
					switch {
					case errors.Is(err, gobreaker.ErrOpenState):
						c.client.logger.Warn("Circuit breaker is open",
							zap.String("provider", selectedProvider.name))
					case errors.Is(err, ErrContentBlocked):
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
				// Prepare parameters
				params = c.prepareParams(params, selectedProvider)

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
					} else {
						c.client.logger.Warn("Failed to create stream",
							zap.String("provider", selectedProvider.name),
							zap.Error(err))
					}
					time.Sleep(1 * time.Second)
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

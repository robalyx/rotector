package verification

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sync"
	"time"

	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/setup/config"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

var (
	ErrNoProxyAssigned      = errors.New("no proxy assigned for verification service token")
	ErrMismatchedTokenCount = errors.New("mismatched token counts between Service A and B")
)

// TokenPair represents a pair of Service A and Service B tokens that work together.
type TokenPair struct {
	ServiceA Service
	ServiceB Service
}

// ServiceManager manages verification service lifecycle and provides access to services.
type ServiceManager struct {
	pairs  []TokenPair
	logger *zap.Logger
}

// NewServiceManager initializes verification services based on configuration.
func NewServiceManager(
	discordConfig config.DiscordConfig, proxyAssignments map[string]*url.URL,
	proxyIndices map[string]int, logger *zap.Logger,
) (*ServiceManager, error) {
	servicesA := make([]Service, 0, len(discordConfig.VerificationServiceA.Tokens))
	servicesB := make([]Service, 0, len(discordConfig.VerificationServiceB.Tokens))

	// Initialize Service A tokens
	for i, tokenConfig := range discordConfig.VerificationServiceA.Tokens {
		proxy, ok := proxyAssignments[tokenConfig.Token]
		if !ok {
			return nil, fmt.Errorf("%w A token %d", ErrNoProxyAssigned, i)
		}

		serviceName := fmt.Sprintf("verification_service_a_%d", i)

		serviceA, err := NewServiceA(Config{
			Token:       tokenConfig.Token,
			GuildID:     tokenConfig.GuildID,
			ChannelID:   tokenConfig.ChannelID,
			CommandName: discordConfig.VerificationServiceA.CommandName,
			ServiceName: serviceName,
		}, proxy, logger)
		if err != nil {
			return nil, fmt.Errorf("failed to create verification service A token %d: %w", i, err)
		}

		servicesA = append(servicesA, serviceA)

		logger.Info("Initialized verification service A token",
			zap.Int("token_index", i),
			zap.Int("proxy_index", proxyIndices[tokenConfig.Token]),
			zap.String("proxy_host", proxy.Host),
			zap.Uint64("guild_id", tokenConfig.GuildID),
			zap.Uint64("channel_id", tokenConfig.ChannelID))
	}

	// Initialize Service B tokens
	for i, tokenConfig := range discordConfig.VerificationServiceB.Tokens {
		proxy, ok := proxyAssignments[tokenConfig.Token]
		if !ok {
			return nil, fmt.Errorf("%w B token %d", ErrNoProxyAssigned, i)
		}

		serviceName := fmt.Sprintf("verification_service_b_%d", i)

		serviceB, err := NewServiceB(Config{
			Token:       tokenConfig.Token,
			GuildID:     tokenConfig.GuildID,
			ChannelID:   tokenConfig.ChannelID,
			CommandName: discordConfig.VerificationServiceB.CommandName,
			ServiceName: serviceName,
		}, proxy, logger)
		if err != nil {
			return nil, fmt.Errorf("failed to create verification service B token %d: %w", i, err)
		}

		servicesB = append(servicesB, serviceB)

		logger.Info("Initialized verification service B token",
			zap.Int("token_index", i),
			zap.Int("proxy_index", proxyIndices[tokenConfig.Token]),
			zap.String("proxy_host", proxy.Host),
			zap.Uint64("guild_id", tokenConfig.GuildID),
			zap.Uint64("channel_id", tokenConfig.ChannelID))
	}

	// Validate equal token counts
	if len(servicesA) != len(servicesB) {
		return nil, fmt.Errorf("%w: Service A has %d tokens, Service B has %d tokens (must be equal)",
			ErrMismatchedTokenCount, len(servicesA), len(servicesB))
	}

	// Create token pairs
	numPairs := len(servicesA)

	pairs := make([]TokenPair, numPairs)
	for i := range numPairs {
		pairs[i] = TokenPair{
			ServiceA: servicesA[i],
			ServiceB: servicesB[i],
		}
		logger.Info("Created token pair",
			zap.Int("pair_index", i),
			zap.String("service_a", servicesA[i].GetServiceName()),
			zap.String("service_b", servicesB[i].GetServiceName()))
	}

	return &ServiceManager{
		pairs:  pairs,
		logger: logger,
	}, nil
}

// Start initializes all verification services.
func (m *ServiceManager) Start(ctx context.Context) error {
	for _, pair := range m.pairs {
		if starter, ok := pair.ServiceA.(interface{ Start(context.Context) error }); ok {
			if err := starter.Start(ctx); err != nil {
				return fmt.Errorf("failed to start verification service A: %w", err)
			}
		}

		if starter, ok := pair.ServiceB.(interface{ Start(context.Context) error }); ok {
			if err := starter.Start(ctx); err != nil {
				return fmt.Errorf("failed to start verification service B: %w", err)
			}
		}
	}

	return nil
}

// Close shuts down all verification services.
func (m *ServiceManager) Close() error {
	for _, pair := range m.pairs {
		if err := pair.ServiceA.Close(); err != nil {
			m.logger.Warn("Failed to close verification service A", zap.Error(err))
		}

		if err := pair.ServiceB.Close(); err != nil {
			m.logger.Warn("Failed to close verification service B", zap.Error(err))
		}
	}

	return nil
}

// GetPairCount returns the number of token pairs available.
func (m *ServiceManager) GetPairCount() int {
	return len(m.pairs)
}

// FetchVerificationProfilesWithPair queries a specific token pair for linked Roblox accounts.
func (m *ServiceManager) FetchVerificationProfilesWithPair(
	ctx context.Context, discordUserID uint64, pairIndex int,
) []*types.DiscordRobloxConnection {
	if pairIndex < 0 || pairIndex >= len(m.pairs) {
		m.logger.Error("Invalid pair index",
			zap.Int("pair_index", pairIndex),
			zap.Int("total_pairs", len(m.pairs)))

		return nil
	}

	pair := m.pairs[pairIndex]
	services := []Service{pair.ServiceA, pair.ServiceB}

	var (
		connections []*types.DiscordRobloxConnection
		mu          sync.Mutex
	)

	g, ctx := errgroup.WithContext(ctx)

	// Query both services in the pair concurrently
	for _, service := range services {
		g.Go(func() error {
			serviceName := service.GetServiceName()

			m.logger.Debug("Attempting verification service",
				zap.Int("pair_index", pairIndex),
				zap.String("service_name", serviceName),
				zap.Uint64("discord_user_id", discordUserID))

			// Execute the verification command
			response, err := service.ExecuteCommand(ctx, discordUserID)
			if err != nil {
				if errors.Is(err, ErrUserNotVerified) {
					m.logger.Debug("User not verified with service",
						zap.String("service_name", serviceName),
						zap.Uint64("discord_user_id", discordUserID))
				} else {
					m.logger.Error("Failed to execute verification command",
						zap.String("service_name", serviceName),
						zap.Uint64("discord_user_id", discordUserID),
						zap.Error(err))
				}

				return nil
			}

			// Parse the response to extract Roblox information
			robloxUserID, robloxUsername, err := service.ParseResponse(response)
			if err != nil {
				// NOTE: usually we would add exponential retry logic but
				// to keep this simple, we will only retry once here
				if errors.Is(err, ErrServiceTemporarilyUnavailable) {
					m.logger.Warn("Service returned temporary error, retrying after delay",
						zap.String("service_name", serviceName),
						zap.Uint64("discord_user_id", discordUserID))

					select {
					case <-time.After(10 * time.Second):
					case <-ctx.Done():
						return nil
					}

					if ctx.Err() != nil {
						return nil
					}

					response, err = service.ExecuteCommand(ctx, discordUserID)
					if err == nil {
						robloxUserID, robloxUsername, err = service.ParseResponse(response)
					}
				}

				if err != nil {
					if errors.Is(err, ErrUserNotVerified) {
						m.logger.Debug("User not verified with service (parse)",
							zap.String("service_name", serviceName),
							zap.Uint64("discord_user_id", discordUserID))
					} else {
						m.logger.Warn("Failed to parse verification response",
							zap.String("service_name", serviceName),
							zap.Uint64("discord_user_id", discordUserID),
							zap.Error(err))
					}

					return nil
				}
			}

			// Success! Add the connection to our results
			m.logger.Info("Successfully verified user with service",
				zap.String("service_name", serviceName),
				zap.Uint64("discord_user_id", discordUserID),
				zap.Int64("roblox_user_id", robloxUserID),
				zap.String("roblox_username", robloxUsername))

			now := time.Now()

			mu.Lock()
			defer mu.Unlock()

			connections = append(connections, &types.DiscordRobloxConnection{
				DiscordUserID:  discordUserID,
				RobloxUserID:   robloxUserID,
				RobloxUsername: robloxUsername,
				Verified:       true,
				DetectedAt:     now,
				UpdatedAt:      now,
			})

			return nil
		})
	}

	// Wait for all services to complete
	_ = g.Wait()

	return connections
}

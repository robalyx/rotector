package verification

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/setup/config"
	"go.uber.org/zap"
)

// ServiceManager manages verification service lifecycle and provides access to services.
type ServiceManager struct {
	services          []Service
	closeableServices []interface{ Close() error }
	logger            *zap.Logger
}

// NewServiceManager initializes verification services based on configuration.
func NewServiceManager(discordConfig config.DiscordConfig, logger *zap.Logger) (*ServiceManager, error) {
	var (
		services          []Service
		closeableServices []interface{ Close() error }
	)

	// Initialize primary verification service
	if discordConfig.VerificationServiceA.Token != "" {
		serviceA, err := NewServiceA(Config{
			Token:       discordConfig.VerificationServiceA.Token,
			GuildID:     discordConfig.VerificationServiceA.GuildID,
			ChannelID:   discordConfig.VerificationServiceA.ChannelID,
			CommandName: discordConfig.VerificationServiceA.CommandName,
			ServiceName: "verification_service_a",
		}, logger)
		if err != nil {
			return nil, fmt.Errorf("failed to create verification service A: %w", err)
		}

		services = append(services, serviceA)
		closeableServices = append(closeableServices, serviceA)

		logger.Info("Initialized verification service A")
	}

	// Initialize secondary verification service
	if discordConfig.VerificationServiceB.Token != "" {
		serviceB, err := NewServiceB(Config{
			Token:       discordConfig.VerificationServiceB.Token,
			GuildID:     discordConfig.VerificationServiceB.GuildID,
			ChannelID:   discordConfig.VerificationServiceB.ChannelID,
			CommandName: discordConfig.VerificationServiceB.CommandName,
			ServiceName: "verification_service_b",
		}, logger)
		if err != nil {
			return nil, fmt.Errorf("failed to create verification service B: %w", err)
		}

		services = append(services, serviceB)
		closeableServices = append(closeableServices, serviceB)

		logger.Info("Initialized verification service B")
	}

	return &ServiceManager{
		services:          services,
		closeableServices: closeableServices,
		logger:            logger,
	}, nil
}

// Start initializes all verification services.
func (m *ServiceManager) Start(ctx context.Context) error {
	for _, service := range m.closeableServices {
		if starter, ok := service.(interface{ Start(context.Context) error }); ok {
			if err := starter.Start(ctx); err != nil {
				return fmt.Errorf("failed to start verification service: %w", err)
			}
		}
	}

	return nil
}

// Close shuts down all verification services.
func (m *ServiceManager) Close() error {
	for _, service := range m.closeableServices {
		if err := service.Close(); err != nil {
			m.logger.Warn("Failed to close verification service", zap.Error(err))
		}
	}

	return nil
}

// GetServices returns the array of verification services for use in scanners.
func (m *ServiceManager) GetServices() []Service {
	return m.services
}

// FetchAllVerificationProfiles queries all external verification services for linked Roblox accounts.
func (m *ServiceManager) FetchAllVerificationProfiles(
	ctx context.Context, discordUserID uint64,
) []*types.DiscordRobloxConnection {
	connections := make([]*types.DiscordRobloxConnection, 0, len(m.services))

	// Attempt each verification service
	for i, service := range m.services {
		serviceName := service.GetServiceName()

		m.logger.Debug("Attempting verification service",
			zap.Int("service_index", i),
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

			continue
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

				time.Sleep(10 * time.Second)

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

				continue
			}
		}

		// Success! Add the connection to our results
		m.logger.Info("Successfully verified user with service",
			zap.String("service_name", serviceName),
			zap.Uint64("discord_user_id", discordUserID),
			zap.Int64("roblox_user_id", robloxUserID),
			zap.String("roblox_username", robloxUsername))

		now := time.Now()

		connections = append(connections, &types.DiscordRobloxConnection{
			DiscordUserID:  discordUserID,
			RobloxUserID:   robloxUserID,
			RobloxUsername: robloxUsername,
			Verified:       true,
			DetectedAt:     now,
			UpdatedAt:      now,
		})
	}

	return connections
}

package verification

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/arikawa/v3/state"
	"github.com/diamondburned/arikawa/v3/utils/httputil"
	"github.com/robalyx/rotector/pkg/utils"
	"github.com/sony/gobreaker"
	"go.uber.org/zap"
)

// ServiceA implements verification service with markdown-based response parsing.
type ServiceA struct {
	executor *BaseExecutor
	state    *state.State
	logger   *zap.Logger
}

// NewServiceA creates a new Service A implementation.
func NewServiceA(config Config, logger *zap.Logger) (*ServiceA, error) {
	executor := NewBaseExecutor(config, logger)

	// Create Discord state with required intents
	s := state.NewWithIntents(
		config.Token,
		gateway.IntentGuilds|gateway.IntentGuildMessages,
	)
	s.UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:136.0) Gecko/20100101 Firefox/136.0"

	service := &ServiceA{
		executor: executor,
		state:    s,
		logger:   logger.Named(config.ServiceName),
	}

	// Register message handlers
	s.AddHandler(executor.HandleMessageCreate)
	s.AddHandler(executor.HandleMessageUpdate)

	return service, nil
}

// Start opens the Discord gateway connection.
func (s *ServiceA) Start(ctx context.Context) error {
	if err := s.state.Open(ctx); err != nil {
		return fmt.Errorf("failed to open gateway: %w", err)
	}

	return nil
}

// ExecuteCommand executes the verification command for a Discord user.
func (s *ServiceA) ExecuteCommand(ctx context.Context, discordUserID uint64) (*Response, error) {
	// Discover command info if not cached
	cmdInfo, err := s.executor.discoverCommand()
	if err != nil {
		return nil, err
	}

	// Generate nonce as unique identifier
	nonce := strconv.FormatInt(time.Now().UnixNano(), 10)

	// Build interaction payload
	payload := map[string]any{
		"type":           2,
		"application_id": cmdInfo["application_id"],
		"guild_id":       strconv.FormatUint(s.executor.guildID, 10),
		"channel_id":     strconv.FormatUint(s.executor.channelID, 10),
		"session_id":     fmt.Sprintf("%x", time.Now().UnixNano()),
		"data": map[string]any{
			"version": cmdInfo["version"],
			"id":      cmdInfo["id"],
			"name":    cmdInfo["name"],
			"type":    cmdInfo["type"],
			"options": []any{
				map[string]any{
					"type":  6,
					"name":  "discord_user",
					"value": strconv.FormatUint(discordUserID, 10),
				},
			},
			"application_command": cmdInfo,
			"attachments":         []any{},
		},
		"nonce":              nonce,
		"analytics_location": "slash_ui",
	}

	// Create response channel and pending request
	responseChan := make(chan *Response, 1)

	s.executor.mu.Lock()
	s.executor.pendingRequests[nonce] = &pendingRequest{
		channel: responseChan,
	}
	s.executor.mu.Unlock()

	// Execute the interaction
	_, err = s.executor.breaker.Execute(func() (any, error) {
		resp, execErr := s.executor.session.Request(
			"POST",
			"https://discord.com/api/v9/interactions",
			httputil.WithJSONBody(payload),
		)
		if execErr != nil {
			return nil, execErr
		}
		defer resp.GetBody().Close()

		return struct{}{}, nil
	})
	if err != nil {
		// Check for circuit breaker open state
		if errors.Is(err, gobreaker.ErrOpenState) {
			s.logger.Warn("Verification API circuit breaker is open, skipping command execution",
				zap.Uint64("discord_user_id", discordUserID))

			return nil, fmt.Errorf("circuit breaker open: %w", err)
		}

		return nil, fmt.Errorf("failed to execute interaction: %w", err)
	}

	s.logger.Info("Executed verification command",
		zap.Uint64("discord_user_id", discordUserID))

	// Wait for response
	select {
	case response := <-responseChan:
		return response, nil
	case <-time.After(30 * time.Second):
		return nil, ErrResponseTimeout
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// ParseResponse extracts Roblox information from markdown-formatted response.
// Expected format: ### [Username](https://www.roblox.com/users/USERID/profile) (USERID).
func (s *ServiceA) ParseResponse(response *Response) (int64, string, error) {
	if len(response.Components) == 0 {
		// Check for service error messages in embeds
		if len(response.Embeds) > 0 {
			for _, embed := range response.Embeds {
				if desc, ok := embed["rawDescription"].(string); ok {
					if strings.Contains(desc, "unable to process") {
						s.logger.Debug("Service returned temporary error embed",
							zap.String("description", desc))

						return 0, "", ErrServiceTemporarilyUnavailable
					}
				}
			}
		}

		// Verify if user is not verified
		if strings.Contains(response.Content, "not verified") {
			return 0, "", ErrUserNotVerified
		}

		return 0, "", ErrUnexpectedResponseFormat
	}

	// Navigate to nested components structure
	// Structure: components[0]["components"][0]["content"]
	outerComponents, ok := response.Components[0]["components"].([]any)
	if !ok || len(outerComponents) == 0 {
		return 0, "", ErrMissingNested
	}

	firstComponent, ok := outerComponents[0].(map[string]any)
	if !ok {
		return 0, "", ErrInvalidFormat
	}

	content, ok := firstComponent["content"].(string)
	if !ok || content == "" {
		return 0, "", ErrMissingContent
	}

	// Parse Roblox user ID and username
	robloxID, username, err := utils.ParseRobloxMarkdown(content)
	if err != nil {
		return 0, "", ErrInvalidRobloxID
	}

	return robloxID, username, nil
}

// GetServiceName returns a generic service name for logging.
func (s *ServiceA) GetServiceName() string {
	return s.executor.GetServiceName()
}

// Close performs cleanup.
func (s *ServiceA) Close() error {
	if err := s.state.Close(); err != nil {
		s.logger.Warn("Failed to close Discord state", zap.Error(err))
	}

	return s.executor.Close()
}

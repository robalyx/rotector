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
	"github.com/sony/gobreaker"
	"go.uber.org/zap"
)

// ServiceB implements verification service with embed-based response parsing.
type ServiceB struct {
	executor *BaseExecutor
	state    *state.State
	logger   *zap.Logger
}

// NewServiceB creates a new Service B implementation.
func NewServiceB(config Config, logger *zap.Logger) (*ServiceB, error) {
	executor := NewBaseExecutor(config, logger)

	// Create Discord state with required intents
	s := state.NewWithIntents(
		config.Token,
		gateway.IntentGuilds|gateway.IntentGuildMessages,
	)
	s.UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:136.0) Gecko/20100101 Firefox/136.0"

	service := &ServiceB{
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
func (s *ServiceB) Start(ctx context.Context) error {
	if err := s.state.Open(ctx); err != nil {
		return fmt.Errorf("failed to open gateway: %w", err)
	}

	return nil
}

// ExecuteCommand executes the verification command for a Discord user.
func (s *ServiceB) ExecuteCommand(ctx context.Context, discordUserID uint64) (*Response, error) {
	// Discover command info if not cached
	cmdInfo, err := s.executor.discoverCommand()
	if err != nil {
		return nil, err
	}

	// Generate nonce as unique identifier
	nonce := strconv.FormatInt(time.Now().UnixNano(), 10)

	// Build interaction payload with nested subcommand structure
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
					"type": 1, // SUB_COMMAND type
					"name": "discord",
					"options": []any{
						map[string]any{
							"type":  6, // USER type
							"name":  "user",
							"value": strconv.FormatUint(discordUserID, 10),
						},
					},
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
	pendingReq := &pendingRequest{
		channel: responseChan,
	}

	s.executor.mu.Lock()
	s.executor.pendingRequests[nonce] = pendingReq
	s.executor.mu.Unlock()

	// Cleanup mappings
	defer func() {
		s.executor.mu.Lock()

		if req, ok := s.executor.pendingRequests[nonce]; ok && req == pendingReq {
			delete(s.executor.pendingRequests, nonce)
		}

		s.executor.mu.Unlock()
	}()

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

// ParseResponse extracts Roblox information from embed-based response.
// Expected format: embeds[0].fields array with "Roblox user ID" and "Roblox username" fields.
func (s *ServiceB) ParseResponse(response *Response) (int64, string, error) {
	// Validate embeds exist
	if len(response.Embeds) == 0 {
		return 0, "", ErrUserNotVerified
	}

	embed := response.Embeds[0]

	// Check if user is not verified
	if rawDesc, ok := embed["rawDescription"].(string); ok {
		if strings.Contains(strings.ToLower(rawDesc), "not verified") {
			return 0, "", ErrUserNotVerified
		}
	}

	// Extract fields array from embed
	fieldsInterface, ok := embed["fields"].([]any)
	if !ok || len(fieldsInterface) == 0 {
		return 0, "", ErrMissingFields
	}

	// Parse fields to find Roblox user ID and username
	var (
		robloxUserIDStr string
		robloxUsername  string
	)

	for _, fieldInterface := range fieldsInterface {
		field, ok := fieldInterface.(map[string]any)
		if !ok {
			continue
		}

		// Attempt REST API format (name/value)
		fieldName, nameOk := field["name"].(string)
		fieldValue, valueOk := field["value"].(string)

		// Or attempt gateway event format (rawName/rawValue)
		// note that it depends on how the response is received
		if !nameOk || !valueOk {
			fieldName, nameOk = field["rawName"].(string)
			fieldValue, valueOk = field["rawValue"].(string)

			if !nameOk || !valueOk {
				continue
			}
		}

		switch fieldName {
		case "Roblox user ID":
			robloxUserIDStr = fieldValue
		case "Roblox username":
			robloxUsername = fieldValue
		}
	}

	// Validate we found both required fields
	if robloxUserIDStr == "" || robloxUsername == "" {
		return 0, "", ErrMissingFields
	}

	// Parse Roblox user ID
	robloxUserID, err := strconv.ParseInt(robloxUserIDStr, 10, 64)
	if err != nil {
		return 0, "", fmt.Errorf("%w: %s", ErrInvalidRobloxID, robloxUserIDStr)
	}

	return robloxUserID, robloxUsername, nil
}

// GetServiceName returns a generic service name for logging.
func (s *ServiceB) GetServiceName() string {
	return s.executor.GetServiceName()
}

// Close performs cleanup.
func (s *ServiceB) Close() error {
	if err := s.state.Close(); err != nil {
		s.logger.Warn("Failed to close Discord state", zap.Error(err))
	}

	return s.executor.Close()
}

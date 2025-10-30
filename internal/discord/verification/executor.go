package verification

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/arikawa/v3/session"
	"github.com/sony/gobreaker"
	"go.uber.org/zap"
)

// CommandIndexResponse represents the guild command index API response.
type CommandIndexResponse struct {
	ApplicationCommands []map[string]any `json:"application_commands"` //nolint:tagliatelle // discord api response
}

// pendingRequest holds the response channel for a pending verification request.
type pendingRequest struct {
	channel chan *Response
}

// BaseExecutor handles command execution for verification services.
type BaseExecutor struct {
	session         *session.Session
	guildID         uint64
	channelID       uint64
	commandName     string
	serviceName     string
	logger          *zap.Logger
	pendingRequests map[string]*pendingRequest // Maps nonce or message ID to pending request
	mu              sync.RWMutex
	commandInfo     map[string]any
	commandMu       sync.RWMutex
	breaker         *gobreaker.CircuitBreaker
}

// NewBaseExecutor creates a new base executor.
func NewBaseExecutor(config Config, logger *zap.Logger) *BaseExecutor {
	serviceLogger := logger.Named(config.ServiceName)

	// Create circuit breaker for verification service API calls
	breaker := gobreaker.NewCircuitBreaker(gobreaker.Settings{
		Name:        config.ServiceName + "_api",
		MaxRequests: 1,
		Timeout:     60 * time.Second,
		Interval:    0,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			failureRatio := float64(counts.TotalFailures) / float64(counts.Requests)
			return counts.Requests >= 10 && failureRatio >= 0.6
		},
		OnStateChange: func(_ string, from gobreaker.State, to gobreaker.State) {
			serviceLogger.Warn("Verification API circuit breaker state changed",
				zap.String("from", from.String()),
				zap.String("to", to.String()))
		},
	})

	// Create Discord session
	sess := session.New(config.Token)
	sess.UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:136.0) Gecko/20100101 Firefox/136.0"

	return &BaseExecutor{
		session:         sess,
		guildID:         config.GuildID,
		channelID:       config.ChannelID,
		commandName:     config.CommandName,
		serviceName:     config.ServiceName,
		logger:          serviceLogger,
		pendingRequests: make(map[string]*pendingRequest),
		breaker:         breaker,
	}
}

// HandleMessageCreate processes message create events for verification responses.
func (e *BaseExecutor) HandleMessageCreate(event *gateway.MessageCreateEvent) {
	e.handleMessage(event.Message)
}

// HandleMessageUpdate processes message update events for verification responses.
func (e *BaseExecutor) HandleMessageUpdate(event *gateway.MessageUpdateEvent) {
	e.handleMessage(event.Message)
}

// GetServiceName returns the service name for logging.
func (e *BaseExecutor) GetServiceName() string {
	return e.serviceName
}

// Close performs cleanup.
func (e *BaseExecutor) Close() error {
	return nil
}

// findVerificationResponse searches recent messages for a verification response matching the message ID.
func (e *BaseExecutor) findVerificationResponse(messageID string) (*Response, error) {
	// Fetch recent messages from channel
	endpoint := fmt.Sprintf("https://discord.com/api/v9/channels/%d/messages?limit=10",
		e.channelID)

	var messages []map[string]any
	if err := e.session.RequestJSON(&messages, "GET", endpoint); err != nil {
		return nil, fmt.Errorf("failed to fetch channel messages: %w", err)
	}

	// Find message by ID
	for _, msg := range messages {
		if msgID, ok := msg["id"].(string); ok && msgID == messageID {
			response := &Response{
				ID: messageID,
			}

			if content, ok := msg["content"].(string); ok {
				response.Content = content
			}

			if components, ok := msg["components"].([]any); ok {
				componentsMap := make([]map[string]any, 0, len(components))
				for _, comp := range components {
					if compMap, ok := comp.(map[string]any); ok {
						componentsMap = append(componentsMap, compMap)
					}
				}

				response.Components = componentsMap
			}

			if embeds, ok := msg["embeds"].([]any); ok {
				embedsMap := make([]map[string]any, 0, len(embeds))
				for _, emb := range embeds {
					if embMap, ok := emb.(map[string]any); ok {
						embedsMap = append(embedsMap, embMap)
					}
				}

				response.Embeds = embedsMap
			}

			e.logger.Debug("Found verification response in channel messages",
				zap.String("message_id", messageID),
				zap.Int("components", len(response.Components)),
				zap.Int("embeds", len(response.Embeds)))

			return response, nil
		}
	}

	return nil, ErrResponseNotFound
}

// handleMessage processes messages for verification responses.
func (e *BaseExecutor) handleMessage(msg discord.Message) {
	// Only process bot messages in the verification channel
	if !msg.Author.Bot || msg.ChannelID != discord.ChannelID(e.channelID) {
		return
	}

	// Check if this is an interaction response
	if msg.Interaction == nil {
		return
	}

	messageID := msg.ID.String()
	nonce := msg.Nonce

	// If message has nonce, this is the initial message create event
	if nonce != "" {
		e.mu.Lock()
		req, ok := e.pendingRequests[nonce]
		e.mu.Unlock()

		if ok {
			// Add message ID mapping for future message update events
			e.mu.Lock()
			e.pendingRequests[messageID] = req
			e.mu.Unlock()

			e.logger.Debug("Mapped nonce to message ID",
				zap.String("nonce", nonce),
				zap.String("message_id", messageID))

			// If thinking state, wait for message update event to confirm completion
			if msg.Flags&128 != 0 {
				e.logger.Debug("Waiting for message update event (thinking state)")
				return
			}
		} else {
			e.logger.Debug("No pending request found for nonce",
				zap.String("nonce", nonce))

			return
		}
	}

	// Look up pending request by message ID
	e.mu.RLock()
	req, ok := e.pendingRequests[messageID]
	e.mu.RUnlock()

	if !ok {
		e.logger.Debug("Ignoring message event for already-processed response",
			zap.String("message_id", messageID))

		return
	}

	// Cleanup mappings
	defer func() {
		e.mu.Lock()
		delete(e.pendingRequests, messageID)

		if nonce != "" {
			delete(e.pendingRequests, nonce)
		}

		e.mu.Unlock()
	}()

	// Fetch full message data from channel messages API
	response, err := e.findVerificationResponse(messageID)
	if err != nil {
		e.logger.Error("Failed to find verification response",
			zap.String("message_id", messageID),
			zap.Error(err))

		return
	}

	ch := req.channel

	// Send response to channel
	select {
	case ch <- response:
		e.logger.Debug("Sent verification response to channel",
			zap.String("message_id", messageID),
			zap.String("content", response.Content),
			zap.Int("components_count", len(response.Components)),
			zap.Int("embeds_count", len(response.Embeds)))
	default:
		e.logger.Warn("Response channel full",
			zap.String("message_id", messageID))
	}
}

// discoverCommand fetches and caches the command information.
func (e *BaseExecutor) discoverCommand() (map[string]any, error) {
	e.commandMu.RLock()

	if e.commandInfo != nil {
		cmd := e.commandInfo
		e.commandMu.RUnlock()

		return cmd, nil
	}

	e.commandMu.RUnlock()

	e.commandMu.Lock()
	defer e.commandMu.Unlock()

	// Double-check after acquiring write lock
	if e.commandInfo != nil {
		return e.commandInfo, nil
	}

	// Fetch command index from API
	endpoint := fmt.Sprintf("https://discord.com/api/v9/guilds/%d/application-command-index", e.guildID)

	var response CommandIndexResponse

	_, err := e.breaker.Execute(func() (any, error) {
		return nil, e.session.RequestJSON(&response, "GET", endpoint)
	})
	if err != nil {
		// Check for circuit breaker open state
		if errors.Is(err, gobreaker.ErrOpenState) {
			e.logger.Warn("Verification API circuit breaker is open, skipping command discovery")
			return nil, fmt.Errorf("circuit breaker open: %w", err)
		}

		return nil, fmt.Errorf("failed to fetch command index: %w", err)
	}

	// Find the command by name
	for _, cmd := range response.ApplicationCommands {
		if name, ok := cmd["name"].(string); ok && name == e.commandName {
			e.commandInfo = cmd
			e.logger.Info("Discovered verification command",
				zap.String("command_name", name),
				zap.String("command_id", cmd["id"].(string)),
				zap.String("app_id", cmd["application_id"].(string)),
				zap.String("version", cmd["version"].(string)))

			return e.commandInfo, nil
		}
	}

	return nil, fmt.Errorf("%w: %s", ErrCommandNotFound, e.commandName)
}

package events

import (
	"sync"

	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/ningen/v3"
	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/robalyx/rotector/internal/ai"
	"github.com/robalyx/rotector/internal/database"
	"github.com/robalyx/rotector/internal/setup"
	"github.com/robalyx/rotector/internal/worker/sync/events/ratelimit"
	"go.uber.org/zap"
)

// Handler manages Discord events and processes messages for analysis.
type Handler struct {
	db               database.Client
	roAPI            *api.API
	state            *ningen.State
	logger           *zap.Logger
	rateLimiter      *ratelimit.Limiter
	messageAnalyzer  *ai.MessageAnalyzer
	guildMessages    map[uint64][]*ai.MessageContent
	messageMu        sync.Mutex
	channelThreshold int
}

// New creates a new event handler.
func New(app *setup.App, state *ningen.State, messageAnalyzer *ai.MessageAnalyzer, logger *zap.Logger) *Handler {
	// Create a new rate limiter with default configuration
	rateLimiter := ratelimit.New(ratelimit.DefaultConfig(), logger)

	// Return a new handler instance
	return &Handler{
		db:               app.DB,
		roAPI:            app.RoAPI,
		state:            state,
		logger:           logger.Named("sync_events"),
		rateLimiter:      rateLimiter,
		messageAnalyzer:  messageAnalyzer,
		guildMessages:    make(map[uint64][]*ai.MessageContent),
		channelThreshold: app.Config.Worker.ThresholdLimits.ChannelProcessThreshold,
	}
}

// Setup registers event handlers.
func (h *Handler) Setup() {
	h.state.AddHandler(h.handleMessageCreate)
	h.logger.Info("Event handler setup complete")
}

// handleMessageCreate processes message creation events to track active users.
func (h *Handler) handleMessageCreate(e *gateway.MessageCreateEvent) {
	// Ignore empty messages, DMs, bot messages, and system messages
	if e.GuildID == 0 || e.Author.Bot || e.WebhookID != 0 || e.Member == nil {
		return
	}

	// Extract information from the event
	serverID := uint64(e.GuildID)
	userID := uint64(e.Author.ID)

	// Check if we should rate limit this event
	if !h.rateLimiter.Allow(userID, serverID) {
		return
	}

	// Queue the message for content analysis
	h.addMessageToQueue(&e.Message)
}

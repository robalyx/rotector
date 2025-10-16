package events

import (
	"context"
	"time"

	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/ningen/v3"
	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/robalyx/rotector/internal/ai"
	"github.com/robalyx/rotector/internal/database"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/setup"
	"github.com/robalyx/rotector/internal/worker/sync/events/ratelimit"
	"go.uber.org/zap"
)

// Handler manages Discord events and processes messages for analysis.
type Handler struct {
	db              database.Client
	roAPI           *api.API
	state           *ningen.State
	logger          *zap.Logger
	rateLimiter     *ratelimit.Limiter
	messageAnalyzer *ai.MessageAnalyzer
}

// New creates a new event handler.
func New(app *setup.App, state *ningen.State, messageAnalyzer *ai.MessageAnalyzer, logger *zap.Logger) *Handler {
	// Create a new rate limiter with default configuration
	rateLimiter := ratelimit.New(ratelimit.DefaultConfig(), logger)

	// Return a new handler instance
	return &Handler{
		db:              app.DB,
		roAPI:           app.RoAPI,
		state:           state,
		logger:          logger.Named("sync_events"),
		rateLimiter:     rateLimiter,
		messageAnalyzer: messageAnalyzer,
	}
}

// Setup registers event handlers.
func (h *Handler) Setup() {
	h.state.AddHandler(h.handleMessageCreate)
	h.logger.Info("Event handler setup complete")
}

// handleMessageCreate processes message creation events to track server members.
func (h *Handler) handleMessageCreate(e *gateway.MessageCreateEvent) {
	// Ignore empty messages, DMs, bot messages, and system messages
	if e.GuildID == 0 || e.Author.Bot || e.WebhookID != 0 || e.Member == nil {
		return
	}

	// Extract information from the event
	serverID := uint64(e.GuildID)
	userID := uint64(e.Author.ID)

	// Check privacy status
	isRedacted, isWhitelisted, err := h.db.Service().Sync().ShouldSkipUser(context.Background(), userID)
	if err != nil {
		h.logger.Error("Failed to check user privacy status",
			zap.Uint64("userID", userID),
			zap.Error(err))

		return
	}

	if isRedacted || isWhitelisted {
		h.logger.Debug("Skipping message from redacted/whitelisted user",
			zap.Uint64("userID", userID))

		return
	}

	// Create server member record
	member := &types.DiscordServerMember{
		ServerID:  serverID,
		UserID:    userID,
		UpdatedAt: time.Now(),
	}

	// Upsert server member
	if err := h.db.Model().Sync().UpsertServerMember(context.Background(), member); err != nil {
		h.logger.Error("Failed to upsert server member",
			zap.Uint64("serverID", serverID),
			zap.Uint64("userID", userID),
			zap.Error(err))

		return
	}

	h.logger.Debug("Added server member from message event",
		zap.Uint64("serverID", serverID),
		zap.Uint64("userID", userID))
}

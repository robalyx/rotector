package events

import (
	"time"

	"github.com/diamondburned/ningen/v3"
	"go.uber.org/zap"

	"github.com/robalyx/rotector/internal/common/storage/database"
	"github.com/robalyx/rotector/internal/worker/sync/events/ratelimit"
)

// Handler manages Discord event handling for member tracking.
type Handler struct {
	db          database.Client
	state       *ningen.State
	logger      *zap.Logger
	rateLimiter *ratelimit.Limiter
}

// New creates a new event handler for member tracking.
func New(db database.Client, state *ningen.State, logger *zap.Logger) *Handler {
	rateLimiter := ratelimit.New(nil, logger.Named("ratelimit"))

	return &Handler{
		db:          db,
		state:       state,
		logger:      logger,
		rateLimiter: rateLimiter,
	}
}

// Setup registers all event handlers for tracking Discord members.
func (h *Handler) Setup() {
	h.state.AddHandler(h.handleGuildMemberRemove)
	h.state.AddHandler(h.handleGuildMemberUpdate)
	h.state.AddHandler(h.handleGuildCreate)
	h.state.AddHandler(h.handleMessageCreate)
	h.state.AddHandler(h.handleTypingStart)
	h.state.AddHandler(h.handleVoiceStateUpdate)
	h.state.AddHandler(h.handleUserUpdate)
	h.state.AddHandler(h.handlePresenceUpdate)

	go h.startRateLimiterCleanup()

	h.logger.Info("Event handlers registered for real-time member tracking")
}

// startRateLimiterCleanup periodically cleans up the rate limiter to prevent memory leaks.
func (h *Handler) startRateLimiterCleanup() {
	ticker := time.NewTicker(12 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		h.logger.Debug("Running rate limiter cleanup")
		h.rateLimiter.Cleanup()
	}
}

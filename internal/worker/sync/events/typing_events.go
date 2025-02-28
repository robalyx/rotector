package events

import (
	"context"
	"time"

	"github.com/diamondburned/arikawa/v3/gateway"
	"go.uber.org/zap"

	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/robalyx/rotector/internal/worker/sync/events/ratelimit"
)

// handleTypingStart processes typing events to track active users.
func (h *Handler) handleTypingStart(e *gateway.TypingStartEvent) {
	// Skip if this is the bot itself, if it's a DM or if it's a bot account
	if e.UserID == h.state.Ready().User.ID || e.GuildID == 0 || e.Member.User.Bot {
		return
	}

	userID := uint64(e.UserID)
	serverID := uint64(e.GuildID)

	// Apply rate limiting
	if !h.rateLimiter.Allow(ratelimit.EventTypeTyping, userID, serverID) {
		return
	}

	// Store member in database
	err := h.db.Models().Sync().UpsertServerMember(context.Background(), &types.DiscordServerMember{
		ServerID:  serverID,
		UserID:    userID,
		JoinedAt:  e.Member.Joined.Time(),
		UpdatedAt: time.Now(),
	})
	if err != nil {
		h.logger.Error("Failed to store member from TypingStart event",
			zap.Uint64("server_id", serverID),
			zap.Uint64("user_id", userID),
			zap.Error(err))
		return
	}

	h.logger.Debug("Updated member from typing activity",
		zap.Uint64("server_id", serverID),
		zap.Uint64("user_id", userID),
		zap.String("channel_id", e.ChannelID.String()))

	// Create a ban record for the user
	h.createBanForUser(userID)
}

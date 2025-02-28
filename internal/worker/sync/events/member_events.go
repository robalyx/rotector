package events

import (
	"context"
	"time"

	"github.com/diamondburned/arikawa/v3/gateway"
	"go.uber.org/zap"

	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/robalyx/rotector/internal/worker/sync/events/ratelimit"
)

// handleGuildMemberRemove tracks when users leave a server.
func (h *Handler) handleGuildMemberRemove(e *gateway.GuildMemberRemoveEvent) {
	// Skip if this is the bot itself or a bot account
	if e.User.ID == h.state.Ready().User.ID || e.User.Bot {
		return
	}

	userID := uint64(e.User.ID)
	serverID := uint64(e.GuildID)

	// Apply rate limiting
	if !h.rateLimiter.Allow(ratelimit.EventTypeMember, userID, serverID) {
		h.logger.Debug("Rate limited member remove event",
			zap.Uint64("server_id", serverID),
			zap.Uint64("user_id", userID))
		return
	}

	// Remove member from database
	err := h.db.Models().Sync().RemoveServerMember(context.Background(), serverID, userID)
	if err != nil {
		h.logger.Error("Failed to remove member from GuildMemberRemove event",
			zap.Uint64("server_id", serverID),
			zap.Uint64("user_id", userID),
			zap.Error(err))
		return
	}

	h.logger.Debug("Removed member from event",
		zap.Uint64("server_id", serverID),
		zap.Uint64("user_id", userID))
}

// handleGuildMemberUpdate tracks when a member's data is updated.
func (h *Handler) handleGuildMemberUpdate(e *gateway.GuildMemberUpdateEvent) {
	// Skip if this is the bot itself or a bot account
	if e.User.ID == h.state.Ready().User.ID || e.User.Bot {
		return
	}

	userID := uint64(e.User.ID)
	serverID := uint64(e.GuildID)

	// Apply rate limiting
	if !h.rateLimiter.Allow(ratelimit.EventTypeMember, userID, serverID) {
		h.logger.Debug("Rate limited member update event",
			zap.Uint64("server_id", serverID),
			zap.Uint64("user_id", userID))
		return
	}

	now := time.Now()

	// Get the member data to access the join date
	member, err := h.state.Member(e.GuildID, e.User.ID)
	if err != nil {
		h.logger.Error("Failed to get member data for update event",
			zap.Uint64("server_id", serverID),
			zap.Uint64("user_id", userID),
			zap.Error(err))
		return
	}

	// Update member in database using the single member method
	err = h.db.Models().Sync().UpsertServerMember(context.Background(), &types.DiscordServerMember{
		ServerID:  serverID,
		UserID:    userID,
		JoinedAt:  member.Joined.Time(),
		UpdatedAt: now,
	})
	if err != nil {
		h.logger.Error("Failed to update member from GuildMemberUpdate event",
			zap.Uint64("server_id", serverID),
			zap.Uint64("user_id", userID),
			zap.Error(err))
		return
	}

	h.logger.Debug("Updated member from event",
		zap.Uint64("server_id", serverID),
		zap.Uint64("user_id", userID))

	// Create a ban record for the user
	h.createBanForUser(userID)
}

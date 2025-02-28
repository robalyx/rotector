package events

import (
	"context"
	"time"

	"github.com/diamondburned/arikawa/v3/gateway"
	"go.uber.org/zap"

	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/robalyx/rotector/internal/worker/sync/events/ratelimit"
)

// handleUserUpdate processes user update events (username, avatar changes, etc.).
func (h *Handler) handleUserUpdate(e *gateway.UserUpdateEvent) {
	// Skip if this is the bot itself or a bot account
	if e.ID == h.state.Ready().User.ID || e.Bot {
		return
	}

	userID := uint64(e.ID)

	// Apply rate limiting - using global rate limiting since this is user-specific
	if !h.rateLimiter.Allow(ratelimit.EventTypeUser, userID, 0) {
		return
	}

	// Check servers this user is in so we can update their member records
	// This requires iterating through all the servers the bot is in
	for _, guild := range h.state.Ready().Guilds {
		// Check if the user is in this guild
		member, err := h.state.Member(guild.ID, e.ID)
		if err != nil {
			// User not in this guild, skip
			continue
		}

		serverID := uint64(guild.ID)
		now := time.Now()

		// Update member record
		err = h.db.Models().Sync().UpsertServerMember(context.Background(), &types.DiscordServerMember{
			ServerID:  serverID,
			UserID:    userID,
			JoinedAt:  member.Joined.Time(),
			UpdatedAt: now,
		})
		if err != nil {
			h.logger.Error("Failed to update member from UserUpdate event",
				zap.Uint64("server_id", serverID),
				zap.Uint64("user_id", userID),
				zap.Error(err))
			continue
		}

		h.logger.Debug("Updated member from user update",
			zap.Uint64("server_id", serverID),
			zap.Uint64("user_id", userID))
	}

	// Create a ban record for the user
	h.createBanForUser(userID)
}

// handlePresenceUpdate processes presence update events (status changes, activities).
func (h *Handler) handlePresenceUpdate(e *gateway.PresenceUpdateEvent) {
	// Skip if this is the bot itself or a bot account
	if e.User.ID == h.state.Ready().User.ID || e.User.Bot {
		return
	}

	userID := uint64(e.User.ID)
	serverID := uint64(e.GuildID)

	// Apply rate limiting
	if !h.rateLimiter.Allow(ratelimit.EventTypePresence, userID, serverID) {
		return
	}

	now := time.Now()

	// Get the member to access join date
	var serverMember *types.DiscordServerMember
	member, err := h.state.Member(e.GuildID, e.User.ID)
	if err != nil {
		serverMember = &types.DiscordServerMember{
			ServerID:  serverID,
			UserID:    userID,
			JoinedAt:  now, // Fallback to current time
			UpdatedAt: now,
		}

		h.logger.Debug("Failed to get member data from presence update",
			zap.Uint64("server_id", serverID),
			zap.Uint64("user_id", userID),
			zap.Error(err))
	} else {
		serverMember = &types.DiscordServerMember{
			ServerID:  serverID,
			UserID:    userID,
			JoinedAt:  member.Joined.Time(),
			UpdatedAt: now,
		}
	}

	// Store member in database
	err = h.db.Models().Sync().UpsertServerMember(context.Background(), serverMember)
	if err != nil {
		h.logger.Error("Failed to store member from PresenceUpdate event",
			zap.Uint64("server_id", serverID),
			zap.Uint64("user_id", userID),
			zap.Error(err))
		return
	}

	h.logger.Debug("Updated member from presence activity",
		zap.Uint64("server_id", serverID),
		zap.Uint64("user_id", userID),
		zap.String("status", string(e.Status)))

	// Create a ban record for the user
	h.createBanForUser(userID)
}

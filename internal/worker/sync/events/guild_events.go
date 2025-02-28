package events

import (
	"context"
	"time"

	"github.com/diamondburned/arikawa/v3/gateway"
	"go.uber.org/zap"

	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/robalyx/rotector/internal/worker/sync/events/ratelimit"
)

// handleGuildCreate processes initial guild data which includes some members.
func (h *Handler) handleGuildCreate(e *gateway.GuildCreateEvent) {
	// Skip processing if no members are included
	if len(e.Members) == 0 {
		return
	}

	serverID := uint64(e.ID)

	// Apply rate limiting for guild events
	if !h.rateLimiter.Allow(ratelimit.EventTypeGuild, 0, serverID) {
		return
	}

	now := time.Now()

	// Extract user IDs to check which ones are already in our database
	userIDs := make([]uint64, 0, len(e.Members))
	for _, member := range e.Members {
		// Skip if this is the bot itself or a bot account
		if member.User.ID == h.state.Ready().User.ID || member.User.Bot {
			continue
		}
		userIDs = append(userIDs, uint64(member.User.ID))
	}

	// Check which users already exist in our database
	existingUsers := make(map[uint64]bool)
	if len(userIDs) > 0 {
		existingMembersMap, err := h.db.Models().Sync().GetFlaggedServerMembers(context.Background(), userIDs)
		if err != nil {
			h.logger.Error("Failed to check existing members",
				zap.Error(err),
				zap.Int("user_count", len(userIDs)))
		} else {
			for userID := range existingMembersMap {
				existingUsers[userID] = true
			}
		}
	}

	members := make([]*types.DiscordServerMember, 0, len(e.Members))
	finalUserIDs := make([]uint64, 0, len(e.Members))

	// Process all members in the event
	for _, member := range e.Members {
		// Skip if this is the bot itself or a bot account
		if member.User.ID == h.state.Ready().User.ID || member.User.Bot {
			continue
		}

		userID := uint64(member.User.ID)

		// Skip grace period if user already exists in our database
		if !existingUsers[userID] {
			// Grace period check - only include members who joined more than 1 hour ago
			oneHourAgo := now.Add(-1 * time.Hour)
			if member.Joined.Time().After(oneHourAgo) {
				h.logger.Debug("Skipping recently joined member (grace period)",
					zap.Uint64("server_id", serverID),
					zap.Uint64("user_id", userID),
					zap.Time("joined_at", member.Joined.Time()))
				continue
			}
		}

		members = append(members, &types.DiscordServerMember{
			ServerID:  serverID,
			UserID:    userID,
			JoinedAt:  member.Joined.Time(),
			UpdatedAt: now,
		})
		finalUserIDs = append(finalUserIDs, userID)
	}

	// Update server info
	err := h.db.Models().Sync().UpsertServerInfo(context.Background(), &types.DiscordServerInfo{
		ServerID:  serverID,
		Name:      e.Name,
		UpdatedAt: now,
	})
	if err != nil {
		h.logger.Error("Failed to store server info from GuildCreate event",
			zap.String("name", e.Name),
			zap.Uint64("id", serverID),
			zap.Error(err))
	}

	// Store members
	if len(members) > 0 {
		err = h.db.Models().Sync().UpsertServerMembers(context.Background(), members)
		if err != nil {
			h.logger.Error("Failed to store members from GuildCreate event",
				zap.String("guild_name", e.Name),
				zap.Uint64("guild_id", serverID),
				zap.Int("member_count", len(members)),
				zap.Error(err))
			return
		}

		h.logger.Debug("Added members from GuildCreate event",
			zap.String("guild_name", e.Name),
			zap.Uint64("guild_id", serverID),
			zap.Int("member_count", len(members)))

		// Create bans for all members
		h.createBulkBansForUsers(finalUserIDs)
	}
}

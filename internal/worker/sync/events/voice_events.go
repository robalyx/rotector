package events

import (
	"context"
	"time"

	"github.com/diamondburned/arikawa/v3/gateway"
	"go.uber.org/zap"

	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/robalyx/rotector/internal/worker/sync/events/ratelimit"
)

// handleVoiceStateUpdate processes voice state changes to track users in voice channels.
func (h *Handler) handleVoiceStateUpdate(e *gateway.VoiceStateUpdateEvent) {
	// Skip if this is the bot itself, if there's no guild ID or if it's a bot account
	if e.UserID == h.state.Ready().User.ID || e.GuildID == 0 || e.Member.User.Bot {
		return
	}

	userID := uint64(e.UserID)
	serverID := uint64(e.GuildID)

	// Apply rate limiting
	if !h.rateLimiter.Allow(ratelimit.EventTypeVoice, userID, serverID) {
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
		h.logger.Error("Failed to store member from VoiceStateUpdate event",
			zap.Uint64("server_id", serverID),
			zap.Uint64("user_id", userID),
			zap.Error(err))
		return
	}

	// Log differently based on whether joining or leaving voice
	channelInfo := "left voice"
	if e.ChannelID != 0 {
		channelInfo = "joined channel " + e.ChannelID.String()
	}

	h.logger.Debug("Updated member from voice activity",
		zap.Uint64("server_id", serverID),
		zap.Uint64("user_id", userID),
		zap.String("voice_action", channelInfo))

	// Create a ban record for the user
	h.createBanForUser(userID)
}

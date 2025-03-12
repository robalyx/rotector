package events

import (
	"context"
	"time"

	"github.com/robalyx/rotector/internal/common/client/ai"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
	"go.uber.org/zap"
)

// storeInappropriateMessages stores flagged messages in the database and updates user summaries.
func (h *Handler) storeInappropriateMessages(
	ctx context.Context, serverID uint64, channelID uint64, flaggedUsers map[uint64]*ai.FlaggedMessageUser,
) error {
	// Create a batch of inappropriate messages
	var messages []*types.InappropriateMessage
	summaries := make([]*types.InappropriateUserSummary, 0, len(flaggedUsers))
	now := time.Now()

	// Process each flagged user
	for userID, flaggedUser := range flaggedUsers {
		// Create a summary for this user
		summary := &types.InappropriateUserSummary{
			UserID:       userID,
			Reason:       flaggedUser.Reason,
			MessageCount: len(flaggedUser.Messages),
			LastDetected: now,
			UpdatedAt:    now,
		}
		summaries = append(summaries, summary)

		// Create a database record for each flagged message
		for _, message := range flaggedUser.Messages {
			messages = append(messages, &types.InappropriateMessage{
				ServerID:   serverID,
				ChannelID:  channelID,
				UserID:     userID,
				MessageID:  message.MessageID,
				Content:    message.Content,
				Reason:     message.Reason,
				Confidence: message.Confidence,
				DetectedAt: now,
				UpdatedAt:  now,
			})
		}
	}

	// Store the messages in the database
	if err := h.db.Models().Message().BatchStoreInappropriateMessages(ctx, messages); err != nil {
		return err
	}

	// Update user summaries
	if err := h.db.Models().Message().BatchUpdateUserSummaries(ctx, summaries); err != nil {
		return err
	}

	h.logger.Info("Stored inappropriate messages",
		zap.Uint64("server_id", serverID),
		zap.Int("user_count", len(flaggedUsers)),
		zap.Int("message_count", len(messages)))
	return nil
}

// CreateBansForUsers creates ban records for multiple users in a single operation.
// It checks which users are already banned and only creates records for new ones.
// This is a shared function used by both the worker and event handlers.
func (h *Handler) CreateBansForUsers(ctx context.Context, userIDs []uint64) {
	if len(userIDs) == 0 {
		return
	}

	now := time.Now()

	// Check which users are already banned
	bannedStatus, err := h.db.Models().Bans().BulkCheckBanned(ctx, userIDs)
	if err != nil {
		h.logger.Error("Failed to check banned status of users",
			zap.Int("user_count", len(userIDs)),
			zap.Error(err))
		return
	}

	// Create ban records for users
	bansToCreate := make([]*types.DiscordBan, 0, len(userIDs))
	for _, userID := range userIDs {
		// Skip if already banned
		if banned, exists := bannedStatus[userID]; exists && banned {
			continue
		}

		// Create a new ban record
		ban := &types.DiscordBan{
			ID:        userID,
			Reason:    enum.BanReasonOther,
			Source:    enum.BanSourceSystem,
			Notes:     "Banned for being in inappropriate Discord server(s)",
			BannedBy:  0, // System ban
			BannedAt:  now,
			ExpiresAt: nil, // Permanent ban
			UpdatedAt: now,
		}
		bansToCreate = append(bansToCreate, ban)
	}

	// Check if we have any bans to create
	if len(bansToCreate) == 0 {
		return
	}

	// Create the bans in the database
	if err := h.db.Models().Bans().BulkBanUsers(ctx, bansToCreate); err != nil {
		h.logger.Error("Failed to create ban records",
			zap.Int("ban_count", len(bansToCreate)),
			zap.Error(err))
		return
	}

	h.logger.Info("Created ban records for users",
		zap.Int("count", len(bansToCreate)))
}

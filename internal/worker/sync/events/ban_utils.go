package events

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
)

// createBanForUser creates a ban record for a user.
func (h *Handler) createBanForUser(userID uint64) {
	ctx := context.Background()
	now := time.Now()

	// First check if user is already banned
	isBanned, err := h.db.Models().Bans().IsBanned(ctx, userID)
	if err != nil {
		h.logger.Error("Failed to check if user is banned",
			zap.Uint64("user_id", userID),
			zap.Error(err))
		return
	}

	// Skip if already banned
	if isBanned {
		return
	}

	// Create ban record
	ban := &types.DiscordBan{
		ID:        userID,
		Reason:    enum.BanReasonInappropriate,
		Source:    enum.BanSourceSystem,
		Notes:     "Automatically banned for being in flagged guilds",
		BannedBy:  0, // System ban
		BannedAt:  now,
		ExpiresAt: nil, // Permanent ban
		UpdatedAt: now,
	}

	err = h.db.Models().Bans().BanUser(ctx, ban)
	if err != nil {
		h.logger.Error("Failed to create ban record for user",
			zap.Uint64("user_id", userID),
			zap.Error(err))
		return
	}

	h.logger.Info("Created automatic ban for user",
		zap.Uint64("user_id", userID))
}

// createBulkBansForUsers creates ban records for multiple users in a single operation.
func (h *Handler) createBulkBansForUsers(userIDs []uint64) {
	if len(userIDs) == 0 {
		return
	}

	ctx := context.Background()
	now := time.Now()

	// Efficiently check which users are already banned
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

		bansToCreate = append(bansToCreate, &types.DiscordBan{
			ID:        userID,
			Reason:    enum.BanReasonInappropriate,
			Source:    enum.BanSourceSystem,
			Notes:     "Automatically banned for being in flagged guilds",
			BannedBy:  0, // System ban
			BannedAt:  now,
			ExpiresAt: nil, // Permanent ban
			UpdatedAt: now,
		})
	}

	// Skip if no new bans to create
	if len(bansToCreate) == 0 {
		return
	}

	// Bulk insert/update ban records
	if err := h.db.Models().Bans().BulkBanUsers(ctx, bansToCreate); err != nil {
		h.logger.Error("Failed to create bulk ban records for users",
			zap.Int("user_count", len(bansToCreate)),
			zap.Error(err))
		return
	}

	h.logger.Info("Created automatic bans for multiple users",
		zap.Int("user_count", len(bansToCreate)))
}

package service

import (
	"context"
	"time"

	"github.com/robalyx/rotector/internal/database/models"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/database/types/enum"
	"go.uber.org/zap"
)

// BanService handles ban-related business logic.
type BanService struct {
	model  *models.BanModel
	logger *zap.Logger
}

// NewBan creates a new ban service.
func NewBan(model *models.BanModel, logger *zap.Logger) *BanService {
	return &BanService{
		model:  model,
		logger: logger.Named("ban_service"),
	}
}

// CreateCondoBans creates ban records for users found in condo servers.
func (b *BanService) CreateCondoBans(ctx context.Context, userIDs []uint64) {
	if len(userIDs) == 0 {
		return
	}

	now := time.Now()

	// Check which users are already banned
	bannedStatus, err := b.model.BulkCheckBanned(ctx, userIDs)
	if err != nil {
		b.logger.Error("Failed to check banned status of users",
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
	if err := b.model.BulkBanUsers(ctx, bansToCreate); err != nil {
		b.logger.Error("Failed to create ban records",
			zap.Int("ban_count", len(bansToCreate)),
			zap.Error(err))

		return
	}

	b.logger.Debug("Created ban records for users",
		zap.Int("count", len(bansToCreate)))
}

package models

import (
	"context"
	"fmt"

	"github.com/robalyx/rotector/internal/database/dbretry"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/uptrace/bun"
	"go.uber.org/zap"
)

// GuildBanModel handles database operations for guild ban logs.
type GuildBanModel struct {
	db     *bun.DB
	logger *zap.Logger
}

// NewGuildBan creates a new guild ban model instance.
func NewGuildBan(db *bun.DB, logger *zap.Logger) *GuildBanModel {
	return &GuildBanModel{
		db:     db,
		logger: logger.Named("db_guild_ban"),
	}
}

// LogBanOperation stores a guild ban operation in the database.
func (m *GuildBanModel) LogBanOperation(ctx context.Context, log *types.GuildBanLog) error {
	err := dbretry.NoResult(ctx, func(ctx context.Context) error {
		_, err := m.db.NewInsert().Model(log).Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to log guild ban operation: %w", err)
		}

		return nil
	})
	if err != nil {
		return err
	}

	m.logger.Debug("Logged guild ban operation",
		zap.Uint64("guildID", log.GuildID),
		zap.Uint64("reviewerID", log.ReviewerID),
		zap.Int("banned_count", log.BannedCount),
		zap.Int("failed_count", log.FailedCount))

	return nil
}

// GetGuildBanLogs retrieves ban logs for a specific guild with pagination.
func (m *GuildBanModel) GetGuildBanLogs(
	ctx context.Context, guildID uint64, cursor *types.LogCursor, limit int,
) ([]*types.GuildBanLog, *types.LogCursor, error) {
	var (
		logs       []*types.GuildBanLog
		nextCursor *types.LogCursor
	)

	err := dbretry.NoResult(ctx, func(ctx context.Context) error {
		// Build base query
		query := m.db.NewSelect().
			Model(&logs).
			Where("guild_id = ?", guildID).
			Limit(limit + 1) // Get one extra to determine if there's a next page

		// Apply cursor conditions if provided
		if cursor != nil {
			query = query.Where("(timestamp, id) <= (?, ?)", cursor.Timestamp, cursor.Sequence)
		}

		// Order by timestamp and ID for stable pagination
		query = query.Order("timestamp DESC", "id DESC")

		err := query.Scan(ctx)
		if err != nil {
			return fmt.Errorf("failed to get guild ban logs: %w", err)
		}

		// Check if we have a next page
		if len(logs) > limit {
			lastLog := logs[limit] // Get the extra log we fetched
			nextCursor = &types.LogCursor{
				Timestamp: lastLog.Timestamp,
				Sequence:  lastLog.ID,
			}
			logs = logs[:limit] // Remove the extra log from results
		}

		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	return logs, nextCursor, nil
}

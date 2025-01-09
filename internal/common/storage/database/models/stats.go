package models

import (
	"context"
	"fmt"
	"time"

	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/uptrace/bun"
	"go.uber.org/zap"
)

// StatsModel handles database operations for statistics.
type StatsModel struct {
	db     *bun.DB
	logger *zap.Logger
}

// NewStats creates a new StatsModel.
func NewStats(db *bun.DB, logger *zap.Logger) *StatsModel {
	return &StatsModel{
		db:     db,
		logger: logger,
	}
}

// GetCurrentStats retrieves the current statistics by counting directly from relevant tables.
func (r *StatsModel) GetCurrentStats(ctx context.Context) (*types.HourlyStats, error) {
	var stats types.HourlyStats
	stats.Timestamp = time.Now().UTC().Truncate(time.Hour)

	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Count confirmed users
		confirmedCount, err := tx.NewSelect().Model((*types.ConfirmedUser)(nil)).Count(ctx)
		if err != nil {
			return err
		}
		stats.UsersConfirmed = int64(confirmedCount)

		// Count flagged users
		flaggedCount, err := tx.NewSelect().Model((*types.FlaggedUser)(nil)).Count(ctx)
		if err != nil {
			return err
		}
		stats.UsersFlagged = int64(flaggedCount)

		// Count cleared users
		clearedCount, err := tx.NewSelect().Model((*types.ClearedUser)(nil)).Count(ctx)
		if err != nil {
			return err
		}
		stats.UsersCleared = int64(clearedCount)

		// Count banned users purged today
		bannedPurgedCount, err := tx.NewSelect().Model((*types.BannedUser)(nil)).
			Where("purged_at >= ?", time.Now().UTC().Truncate(24*time.Hour)).
			Count(ctx)
		if err != nil {
			return err
		}
		stats.UsersBanned = int64(bannedPurgedCount)

		// Count confirmed groups
		confirmedGroupsCount, err := tx.NewSelect().Model((*types.ConfirmedGroup)(nil)).Count(ctx)
		if err != nil {
			return err
		}
		stats.GroupsConfirmed = int64(confirmedGroupsCount)

		// Count flagged groups
		flaggedGroupsCount, err := tx.NewSelect().Model((*types.FlaggedGroup)(nil)).Count(ctx)
		if err != nil {
			return err
		}
		stats.GroupsFlagged = int64(flaggedGroupsCount)

		// Count cleared groups
		clearedGroupsCount, err := tx.NewSelect().Model((*types.ClearedGroup)(nil)).Count(ctx)
		if err != nil {
			return err
		}
		stats.GroupsCleared = int64(clearedGroupsCount)

		// Count groups locked today
		lockedGroupsCount, err := tx.NewSelect().Model((*types.FlaggedGroup)(nil)).
			Where("last_purge_check >= ? AND last_purge_check < ?",
				time.Now().UTC().Truncate(24*time.Hour),
				time.Now().UTC()).
			Count(ctx)
		if err != nil {
			return err
		}
		stats.GroupsLocked = int64(lockedGroupsCount)

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get current stats: %w", err)
	}

	return &stats, nil
}

// SaveHourlyStats saves the current statistics snapshot.
func (r *StatsModel) SaveHourlyStats(ctx context.Context, stats *types.HourlyStats) error {
	_, err := r.db.NewInsert().
		Model(stats).
		On("CONFLICT (timestamp) DO UPDATE").
		Set("users_confirmed = EXCLUDED.users_confirmed").
		Set("users_flagged = EXCLUDED.users_flagged").
		Set("users_cleared = EXCLUDED.users_cleared").
		Set("users_banned = EXCLUDED.users_banned").
		Set("groups_confirmed = EXCLUDED.groups_confirmed").
		Set("groups_flagged = EXCLUDED.groups_flagged").
		Set("groups_cleared = EXCLUDED.groups_cleared").
		Set("groups_locked = EXCLUDED.groups_locked").
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to save hourly stats: %w", err)
	}

	return nil
}

// GetHourlyStats retrieves hourly statistics for the last 24 hours.
func (r *StatsModel) GetHourlyStats(ctx context.Context) ([]*types.HourlyStats, error) {
	var stats []*types.HourlyStats
	now := time.Now().UTC()
	dayAgo := now.Add(-24 * time.Hour)

	err := r.db.NewSelect().
		Model(&stats).
		Where("timestamp >= ? AND timestamp <= ?", dayAgo, now).
		Order("timestamp ASC").
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get hourly stats: %w", err)
	}

	return stats, nil
}

// HasStatsForHour checks if statistics exist for a specific hour.
func (r *StatsModel) HasStatsForHour(ctx context.Context, hour time.Time) (bool, error) {
	exists, err := r.db.NewSelect().
		Model((*types.HourlyStats)(nil)).
		Where("timestamp = ?", hour.UTC().Truncate(time.Hour)).
		Exists(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to check stats existence for hour %v: %w", hour, err)
	}
	return exists, nil
}

// PurgeOldStats removes statistics older than the cutoff date.
func (r *StatsModel) PurgeOldStats(ctx context.Context, cutoffDate time.Time) error {
	result, err := r.db.NewDelete().
		Model((*types.HourlyStats)(nil)).
		Where("timestamp < ?", cutoffDate).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to purge old stats: %w (cutoffDate=%s)", err, cutoffDate.Format(time.RFC3339))
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w (cutoffDate=%s)", err, cutoffDate.Format(time.RFC3339))
	}

	r.logger.Debug("Purged old stats",
		zap.Int64("rowsAffected", rowsAffected),
		zap.Time("cutoffDate", cutoffDate))
	return nil
}

// GetCurrentCounts retrieves all current user and group counts in a single transaction.
func (r *StatsModel) GetCurrentCounts(ctx context.Context) (*types.UserCounts, *types.GroupCounts, error) {
	var userCounts types.UserCounts
	var groupCounts types.GroupCounts

	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Get user counts
		confirmedCount, err := tx.NewSelect().Model((*types.ConfirmedUser)(nil)).Count(ctx)
		if err != nil {
			return fmt.Errorf("failed to get confirmed users count: %w", err)
		}
		userCounts.Confirmed = confirmedCount

		flaggedCount, err := tx.NewSelect().Model((*types.FlaggedUser)(nil)).Count(ctx)
		if err != nil {
			return fmt.Errorf("failed to get flagged users count: %w", err)
		}
		userCounts.Flagged = flaggedCount

		clearedCount, err := tx.NewSelect().Model((*types.ClearedUser)(nil)).Count(ctx)
		if err != nil {
			return fmt.Errorf("failed to get cleared users count: %w", err)
		}
		userCounts.Cleared = clearedCount

		bannedCount, err := tx.NewSelect().Model((*types.BannedUser)(nil)).Count(ctx)
		if err != nil {
			return fmt.Errorf("failed to get banned users count: %w", err)
		}
		userCounts.Banned = bannedCount

		// Get group counts
		confirmedGroupCount, err := tx.NewSelect().Model((*types.ConfirmedGroup)(nil)).Count(ctx)
		if err != nil {
			return fmt.Errorf("failed to get confirmed groups count: %w", err)
		}
		groupCounts.Confirmed = confirmedGroupCount

		flaggedGroupCount, err := tx.NewSelect().Model((*types.FlaggedGroup)(nil)).Count(ctx)
		if err != nil {
			return fmt.Errorf("failed to get flagged groups count: %w", err)
		}
		groupCounts.Flagged = flaggedGroupCount

		clearedGroupCount, err := tx.NewSelect().Model((*types.ClearedGroup)(nil)).Count(ctx)
		if err != nil {
			return fmt.Errorf("failed to get cleared groups count: %w", err)
		}
		groupCounts.Cleared = clearedGroupCount

		lockedGroupCount, err := tx.NewSelect().Model((*types.LockedGroup)(nil)).Count(ctx)
		if err != nil {
			return fmt.Errorf("failed to get locked groups count: %w", err)
		}
		groupCounts.Locked = lockedGroupCount

		return nil
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get current counts: %w", err)
	}

	return &userCounts, &groupCounts, nil
}

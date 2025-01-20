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
	users  *UserModel
	groups *GroupModel
	logger *zap.Logger
}

// NewStats creates a new StatsModel.
func NewStats(db *bun.DB, users *UserModel, groups *GroupModel, logger *zap.Logger) *StatsModel {
	return &StatsModel{
		db:     db,
		users:  users,
		groups: groups,
		logger: logger,
	}
}

// GetCurrentStats retrieves the current statistics by counting directly from relevant tables.
func (r *StatsModel) GetCurrentStats(ctx context.Context) (*types.HourlyStats, error) {
	var stats types.HourlyStats
	stats.Timestamp = time.Now().UTC().Truncate(time.Hour)

	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Count confirmed users
		confirmedUserCount, err := tx.NewSelect().Model((*types.ConfirmedUser)(nil)).Count(ctx)
		if err != nil {
			return err
		}
		stats.UsersConfirmed = int64(confirmedUserCount)

		// Count flagged users
		flaggedUserCount, err := tx.NewSelect().Model((*types.FlaggedUser)(nil)).Count(ctx)
		if err != nil {
			return err
		}
		stats.UsersFlagged = int64(flaggedUserCount)

		// Count cleared users
		clearedUserCount, err := tx.NewSelect().Model((*types.ClearedUser)(nil)).Count(ctx)
		if err != nil {
			return err
		}
		stats.UsersCleared = int64(clearedUserCount)

		// Count banned users
		bannedUserCount, err := r.users.GetBannedCount(ctx)
		if err != nil {
			return err
		}
		stats.UsersBanned = int64(bannedUserCount)

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

		// Count locked groups
		lockedGroupsCount, err := r.groups.GetLockedCount(ctx)
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

// GetCurrentCounts retrieves all current user and group counts.
func (r *StatsModel) GetCurrentCounts(ctx context.Context) (*types.UserCounts, *types.GroupCounts, error) {
	userCounts, err := r.users.GetUserCounts(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get user counts: %w", err)
	}

	groupCounts, err := r.groups.GetGroupCounts(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get group counts: %w", err)
	}

	return userCounts, groupCounts, nil
}

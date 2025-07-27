package models

import (
	"context"
	"fmt"
	"time"

	"github.com/robalyx/rotector/internal/database/dbretry"
	"github.com/robalyx/rotector/internal/database/types"
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
		logger: logger.Named("db_stats"),
	}
}

// SaveHourlyStats saves the current statistics snapshot.
//
// Deprecated: Use Service().Stats().SaveHourlyStats() instead.
func (r *StatsModel) SaveHourlyStats(ctx context.Context, stats *types.HourlyStats) error {
	return dbretry.NoResult(ctx, func(ctx context.Context) error {
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
	})
}

// GetHourlyStats retrieves hourly statistics for the last 24 hours.
func (r *StatsModel) GetHourlyStats(ctx context.Context) ([]*types.HourlyStats, error) {
	return dbretry.Operation(ctx, func(ctx context.Context) ([]*types.HourlyStats, error) {
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
	})
}

// HasStatsForHour checks if statistics exist for a specific hour.
func (r *StatsModel) HasStatsForHour(ctx context.Context, hour time.Time) (bool, error) {
	return dbretry.Operation(ctx, func(ctx context.Context) (bool, error) {
		exists, err := r.db.NewSelect().
			Model((*types.HourlyStats)(nil)).
			Where("timestamp = ?", hour.UTC().Truncate(time.Hour)).
			Exists(ctx)
		if err != nil {
			return false, fmt.Errorf("failed to check stats existence for hour %v: %w", hour, err)
		}

		return exists, nil
	})
}

// PurgeOldStats removes statistics older than the cutoff date.
func (r *StatsModel) PurgeOldStats(ctx context.Context, cutoffDate time.Time) error {
	return dbretry.NoResult(ctx, func(ctx context.Context) error {
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
	})
}

package database

import (
	"context"
	"time"

	"github.com/uptrace/bun"
	"go.uber.org/zap"
)

// HourlyStats stores cumulative statistics for each hour.
type HourlyStats struct {
	Timestamp          time.Time `bun:",pk"`
	UsersConfirmed     int64     `bun:",notnull"`
	UsersFlagged       int64     `bun:",notnull"`
	UsersCleared       int64     `bun:",notnull"`
	BannedUsersPurged  int64     `bun:",notnull"`
	FlaggedUsersPurged int64     `bun:",notnull"`
	ClearedUsersPurged int64     `bun:",notnull"`
}

// StatsRepository handles database operations for statistics.
type StatsRepository struct {
	db     *bun.DB
	logger *zap.Logger
}

// NewStatsRepository creates a new StatsRepository.
func NewStatsRepository(db *bun.DB, logger *zap.Logger) *StatsRepository {
	return &StatsRepository{
		db:     db,
		logger: logger,
	}
}

// GetCurrentStats retrieves the current statistics by counting directly from relevant tables.
func (r *StatsRepository) GetCurrentStats(ctx context.Context) (*HourlyStats, error) {
	var stats HourlyStats
	stats.Timestamp = time.Now().UTC().Truncate(time.Hour)

	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Count confirmed users
		confirmedCount, err := tx.NewSelect().Model((*ConfirmedUser)(nil)).Count(ctx)
		if err != nil {
			return err
		}
		stats.UsersConfirmed = int64(confirmedCount)

		// Count flagged users
		flaggedCount, err := tx.NewSelect().Model((*FlaggedUser)(nil)).Count(ctx)
		if err != nil {
			return err
		}
		stats.UsersFlagged = int64(flaggedCount)

		// Count cleared users
		clearedCount, err := tx.NewSelect().Model((*ClearedUser)(nil)).Count(ctx)
		if err != nil {
			return err
		}
		stats.UsersCleared = int64(clearedCount)

		// Count banned users purged today
		bannedPurgedCount, err := tx.NewSelect().Model((*BannedUser)(nil)).
			Where("purged_at >= ?", time.Now().UTC().Truncate(24*time.Hour)).
			Count(ctx)
		if err != nil {
			return err
		}
		stats.BannedUsersPurged = int64(bannedPurgedCount)

		// Count flagged users purged today
		flaggedPurgedCount, err := tx.NewSelect().Model((*FlaggedUser)(nil)).
			Where("last_purge_check >= ?", time.Now().UTC().Truncate(24*time.Hour)).
			Count(ctx)
		if err != nil {
			return err
		}
		stats.FlaggedUsersPurged = int64(flaggedPurgedCount)

		// Count cleared users purged today
		clearedPurgedCount, err := tx.NewSelect().Model((*ClearedUser)(nil)).
			Where("cleared_at >= ?", time.Now().UTC().Truncate(24*time.Hour)).
			Count(ctx)
		if err != nil {
			return err
		}
		stats.ClearedUsersPurged = int64(clearedPurgedCount)

		return nil
	})
	if err != nil {
		r.logger.Error("Failed to get current stats", zap.Error(err))
		return nil, err
	}

	return &stats, nil
}

// SaveHourlyStats saves the current statistics snapshot.
func (r *StatsRepository) SaveHourlyStats(ctx context.Context, stats *HourlyStats) error {
	_, err := r.db.NewInsert().
		Model(stats).
		On("CONFLICT (timestamp) DO UPDATE").
		Set("users_confirmed = EXCLUDED.users_confirmed").
		Set("users_flagged = EXCLUDED.users_flagged").
		Set("users_cleared = EXCLUDED.users_cleared").
		Set("banned_users_purged = EXCLUDED.banned_users_purged").
		Set("flagged_users_purged = EXCLUDED.flagged_users_purged").
		Set("cleared_users_purged = EXCLUDED.cleared_users_purged").
		Exec(ctx)
	if err != nil {
		r.logger.Error("Failed to save hourly stats", zap.Error(err))
		return err
	}

	return nil
}

// GetHourlyStats retrieves hourly statistics for the last 24 hours.
func (r *StatsRepository) GetHourlyStats(ctx context.Context) ([]HourlyStats, error) {
	var stats []HourlyStats
	now := time.Now().UTC()
	dayAgo := now.Add(-24 * time.Hour)

	err := r.db.NewSelect().
		Model(&stats).
		Where("timestamp >= ? AND timestamp <= ?", dayAgo, now).
		Order("timestamp ASC").
		Scan(ctx)
	if err != nil {
		r.logger.Error("Failed to get hourly stats", zap.Error(err))
		return nil, err
	}

	return stats, nil
}

// PurgeOldStats removes statistics older than the cutoff date.
func (r *StatsRepository) PurgeOldStats(ctx context.Context, cutoffDate time.Time) error {
	result, err := r.db.NewDelete().
		Model((*HourlyStats)(nil)).
		Where("timestamp < ?", cutoffDate).
		Exec(ctx)
	if err != nil {
		r.logger.Error("Failed to purge old stats",
			zap.Error(err),
			zap.Time("cutoffDate", cutoffDate))
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		r.logger.Error("Failed to get rows affected", zap.Error(err))
		return err
	}

	r.logger.Info("Purged old stats",
		zap.Int64("rowsAffected", rowsAffected),
		zap.Time("cutoffDate", cutoffDate))
	return nil
}

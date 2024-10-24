package database

import (
	"context"
	"fmt"
	"time"

	"github.com/go-pg/pg/v10"
	"github.com/redis/rueidis"
	"go.uber.org/zap"
)

// StatsRepository handles statistics-related database operations.
type StatsRepository struct {
	db     *pg.DB
	redis  rueidis.Client
	logger *zap.Logger
}

// NewStatsRepository creates a new StatsRepository instance.
func NewStatsRepository(db *pg.DB, redis rueidis.Client, logger *zap.Logger) *StatsRepository {
	return &StatsRepository{
		db:     db,
		redis:  redis,
		logger: logger,
	}
}

// UploadDailyStatsToDB uploads daily statistics from Redis to PostgreSQL.
func (r *StatsRepository) UploadDailyStatsToDB(ctx context.Context) error {
	// Get the Redis key for yesterday's statistics
	date := time.Now().Add(-24 * time.Hour).Format("2006-01-02")
	key := "daily_statistics:" + date

	// Get the daily statistics from Redis
	cmd := r.redis.B().Hgetall().Key(key).Build()
	result, err := r.redis.Do(ctx, cmd).AsIntMap()
	if err != nil {
		return fmt.Errorf("failed to get daily stats from Redis: %w", err)
	}

	// Log the raw result
	r.logger.Info("Raw Redis result", zap.Any("result", result), zap.String("key", key))

	// If the result is empty, log a warning
	if len(result) == 0 {
		r.logger.Warn("Redis returned an empty result", zap.String("key", key))
		return nil
	}

	// Create a new DailyStatistics instance
	stats := &DailyStatistics{
		Date:         time.Now().AddDate(0, 0, -1),
		UsersBanned:  result["users_banned"],
		UsersCleared: result["users_cleared"],
		UsersFlagged: result["users_flagged"],
		UsersPurged:  result["users_purged"],
	}

	// Insert the daily statistics into PostgreSQL
	_, err = r.db.Model(stats).OnConflict("(date) DO UPDATE").Insert()
	if err != nil {
		return fmt.Errorf("failed to insert daily stats into PostgreSQL: %w", err)
	}

	// Delete the Redis key after successful upload
	delCmd := r.redis.B().Del().Key(key).Build()
	if err := r.redis.Do(ctx, delCmd).Error(); err != nil {
		r.logger.Error("Failed to delete Redis key after upload", zap.Error(err))
	}

	return nil
}

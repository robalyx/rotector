package models

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/robalyx/rotector/internal/database/dbretry"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/uptrace/bun"
	"go.uber.org/zap"
)

const (
	// FriendCountTTL defines how long friend counts remain valid.
	FriendCountTTL = 7 * 24 * time.Hour
)

// CacheModel handles database operations for caching user data to optimize worker performance.
type CacheModel struct {
	db     *bun.DB
	logger *zap.Logger
}

// NewCache creates a CacheModel for managing cache entries.
func NewCache(db *bun.DB, logger *zap.Logger) *CacheModel {
	return &CacheModel{
		db:     db,
		logger: logger.Named("db_cache"),
	}
}

// GetFriendCount retrieves a user's cached friend count.
// Returns the count and true if found and fresh (within TTL), or 0 and false otherwise.
func (r *CacheModel) GetFriendCount(ctx context.Context, userID int64) (int, bool, error) {
	var entry types.UserFriendCount

	err := dbretry.NoResult(ctx, func(ctx context.Context) error {
		return r.db.NewSelect().
			Model(&entry).
			Where("user_id = ?", userID).
			Where("last_updated > ?", time.Now().Add(-FriendCountTTL)).
			Scan(ctx)
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Record not found or stale - this is expected
			return 0, false, nil
		}

		return 0, false, fmt.Errorf("failed to get friend count for user %d: %w", userID, err)
	}

	r.logger.Debug("Retrieved friend count from cache",
		zap.Int64("userID", userID),
		zap.Int("friendCount", entry.FriendCount))

	return entry.FriendCount, true, nil
}

// SetFriendCount caches a user's current friend count.
func (r *CacheModel) SetFriendCount(ctx context.Context, userID int64, count int) error {
	err := dbretry.NoResult(ctx, func(ctx context.Context) error {
		entry := &types.UserFriendCount{
			UserID:      userID,
			FriendCount: count,
			LastUpdated: time.Now(),
		}

		_, err := r.db.NewInsert().
			Model(entry).
			On("CONFLICT (user_id) DO UPDATE").
			Set("friend_count = EXCLUDED.friend_count").
			Set("last_updated = EXCLUDED.last_updated").
			Exec(ctx)

		return err
	})
	if err != nil {
		return fmt.Errorf("failed to set friend count for user %d: %w", userID, err)
	}

	r.logger.Debug("Stored friend count in cache",
		zap.Int64("userID", userID),
		zap.Int("friendCount", count))

	return nil
}

// HasFriendCountChanged compares current friend count with cached value.
// Returns true if the count has changed or no valid cache entry exists.
func (r *CacheModel) HasFriendCountChanged(ctx context.Context, userID int64, currentCount int) (bool, error) {
	cachedCount, exists, err := r.GetFriendCount(ctx, userID)
	if err != nil {
		// Assume changed on error to be safe
		return true, err
	}

	if !exists {
		r.logger.Debug("No cached friend count, assuming changed",
			zap.Int64("userID", userID),
			zap.Int("currentCount", currentCount))

		return true, nil
	}

	changed := cachedCount != currentCount

	r.logger.Debug("Friend count comparison",
		zap.Int64("userID", userID),
		zap.Int("cachedCount", cachedCount),
		zap.Int("currentCount", currentCount),
		zap.Bool("changed", changed))

	return changed, nil
}

// GetProcessingLogs retrieves processing log entries for the given user IDs.
func (r *CacheModel) GetProcessingLogs(ctx context.Context, userIDs []int64) ([]types.UserProcessingLog, error) {
	if len(userIDs) == 0 {
		return nil, nil
	}

	var processedEntries []types.UserProcessingLog

	err := dbretry.NoResult(ctx, func(ctx context.Context) error {
		return r.db.NewSelect().
			Model(&processedEntries).
			Column("user_id", "last_processed", "next_scan_time").
			Where("user_id IN (?)", bun.In(userIDs)).
			Scan(ctx)
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}

		return nil, fmt.Errorf("failed to query processing logs: %w", err)
	}

	return processedEntries, nil
}

// MarkUsersProcessed marks the given users as processed with pre-calculated next scan times.
func (r *CacheModel) MarkUsersProcessed(ctx context.Context, entries []*types.UserProcessingLog) error {
	if len(entries) == 0 {
		return nil
	}

	err := dbretry.NoResult(ctx, func(ctx context.Context) error {
		_, err := r.db.NewInsert().
			Model(&entries).
			On("CONFLICT (user_id) DO UPDATE").
			Set("last_processed = EXCLUDED.last_processed").
			Set("next_scan_time = EXCLUDED.next_scan_time").
			Exec(ctx)

		return err
	})
	if err != nil {
		return fmt.Errorf("failed to mark users as processed: %w", err)
	}

	r.logger.Debug("Successfully marked all users as processed",
		zap.Int("userCount", len(entries)))

	return nil
}

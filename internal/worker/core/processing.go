package core

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/rueidis"
	"github.com/robalyx/rotector/internal/redis"
	"go.uber.org/zap"
)

const (
	// ProcessingTTL is the time to live for processed user entries (24 hours).
	ProcessingTTL = 24 * time.Hour

	// ProcessingKeyPrefix is the Redis key prefix for processed users.
	ProcessingKeyPrefix = "processed_user:"
)

// ErrPartialMarkFailed indicates some users failed to be marked as processed.
var ErrPartialMarkFailed = errors.New("some users failed to be marked as processed")

// UserProcessingCache handles Redis-based caching to prevent duplicate
// user processing within a 24-hour window across friend and group workers.
type UserProcessingCache struct {
	client rueidis.Client
	logger *zap.Logger
}

// NewUserProcessingCache creates a new user processing cache instance.
func NewUserProcessingCache(redisManager *redis.Manager, logger *zap.Logger) *UserProcessingCache {
	client, err := redisManager.GetClient(redis.ProcessingDBIndex)
	if err != nil {
		logger.Fatal("Failed to get Redis client for user processing cache", zap.Error(err))
	}

	return &UserProcessingCache{
		client: client,
		logger: logger.Named("user_processing_cache"),
	}
}

// FilterProcessedUsers filters out user IDs that have already been processed
// within the TTL window, returning only unprocessed user IDs.
func (u *UserProcessingCache) FilterProcessedUsers(ctx context.Context, userIDs []uint64) ([]uint64, error) {
	if len(userIDs) == 0 {
		return userIDs, nil
	}

	u.logger.Debug("Filtering processed users", zap.Int("totalUsers", len(userIDs)))

	// Check each user ID individually
	var unprocessedUsers []uint64

	cacheHits := 0

	for _, userID := range userIDs {
		key := ProcessingKeyPrefix + strconv.FormatUint(userID, 10)

		// Check if key exists in Redis
		exists, err := u.client.Do(ctx, u.client.B().Exists().Key(key).Build()).AsBool()
		if err != nil {
			u.logger.Warn("Failed to check if user processed in Redis",
				zap.Uint64("userID", userID),
				zap.Error(err))
			unprocessedUsers = append(unprocessedUsers, userID)

			continue
		}

		if !exists {
			// User not in cache, include for processing
			unprocessedUsers = append(unprocessedUsers, userID)
		} else {
			// User already processed, skip
			cacheHits++
		}
	}

	u.logger.Info("Filtered processed users",
		zap.Int("totalUsers", len(userIDs)),
		zap.Int("unprocessedUsers", len(unprocessedUsers)),
		zap.Int("cacheHits", cacheHits),
		zap.Float64("cacheHitRate", float64(cacheHits)/float64(len(userIDs))*100))

	return unprocessedUsers, nil
}

// MarkUsersProcessed marks the given user IDs as processed in Redis
// with the configured TTL to prevent reprocessing within 24 hours.
func (u *UserProcessingCache) MarkUsersProcessed(ctx context.Context, userIDs []uint64) error {
	if len(userIDs) == 0 {
		return nil
	}

	u.logger.Debug("Marking users as processed", zap.Int("userCount", len(userIDs)))

	currentTime := strconv.FormatInt(time.Now().Unix(), 10)
	failedCount := 0

	// Set each user ID individually
	for _, userID := range userIDs {
		key := ProcessingKeyPrefix + strconv.FormatUint(userID, 10)

		// Set the key with TTL
		err := u.client.Do(ctx, u.client.B().Set().Key(key).Value(currentTime).Ex(ProcessingTTL).Build()).Error()
		if err != nil {
			u.logger.Warn("Failed to set processed status for user",
				zap.Uint64("userID", userID),
				zap.Error(err))

			failedCount++
		}
	}

	if failedCount > 0 {
		u.logger.Warn("Some users failed to be marked as processed",
			zap.Int("failedCount", failedCount),
			zap.Int("totalCount", len(userIDs)))

		return fmt.Errorf("failed to mark %d out of %d users as processed: %w", failedCount, len(userIDs), ErrPartialMarkFailed)
	}

	u.logger.Debug("Successfully marked all users as processed",
		zap.Int("userCount", len(userIDs)))

	return nil
}

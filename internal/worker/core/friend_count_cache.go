package core

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/rueidis"
	"github.com/robalyx/rotector/internal/redis"
	"go.uber.org/zap"
)

const (
	// FriendCountTTL defines how long friend counts remain cached.
	FriendCountTTL = 7 * 24 * time.Hour

	// FriendCountKeyPrefix identifies friend count entries in Redis.
	FriendCountKeyPrefix = "friend_count:"
)

// FriendCountCache tracks user friend counts in Redis to optimize
// friend worker processing by avoiding redundant friend list fetches
// when the count hasn't changed.
type FriendCountCache struct {
	client rueidis.Client
	logger *zap.Logger
}

// NewFriendCountCache initializes the friend count cache.
func NewFriendCountCache(redisManager *redis.Manager, logger *zap.Logger) *FriendCountCache {
	client, err := redisManager.GetClient(redis.ProcessingDBIndex)
	if err != nil {
		logger.Fatal("Failed to get Redis client for friend count cache", zap.Error(err))
	}

	return &FriendCountCache{
		client: client,
		logger: logger.Named("friend_count_cache"),
	}
}

// GetFriendCount retrieves a user's cached friend count.
// Returns the count and true if found, or 0 and false if not cached.
func (f *FriendCountCache) GetFriendCount(ctx context.Context, userID int64) (int, bool, error) {
	key := FriendCountKeyPrefix + strconv.FormatInt(userID, 10)

	countStr, err := f.client.Do(ctx, f.client.B().Get().Key(key).Build()).ToString()
	if err != nil {
		if rueidis.IsRedisNil(err) {
			return 0, false, nil
		}

		f.logger.Warn("Failed to get friend count from Redis",
			zap.Int64("userID", userID),
			zap.Error(err))

		return 0, false, fmt.Errorf("failed to get friend count for user %d: %w", userID, err)
	}

	count, err := strconv.Atoi(countStr)
	if err != nil {
		f.logger.Warn("Invalid friend count value in Redis",
			zap.Int64("userID", userID),
			zap.String("value", countStr),
			zap.Error(err))

		return 0, false, fmt.Errorf("invalid friend count value for user %d: %w", userID, err)
	}

	f.logger.Debug("Retrieved friend count from cache",
		zap.Int64("userID", userID),
		zap.Int("friendCount", count))

	return count, true, nil
}

// SetFriendCount caches a user's current friend count.
func (f *FriendCountCache) SetFriendCount(ctx context.Context, userID int64, count int) error {
	key := FriendCountKeyPrefix + strconv.FormatInt(userID, 10)
	countStr := strconv.Itoa(count)

	err := f.client.Do(ctx, f.client.B().Set().Key(key).Value(countStr).Ex(FriendCountTTL).Build()).Error()
	if err != nil {
		f.logger.Warn("Failed to set friend count in Redis",
			zap.Int64("userID", userID),
			zap.Int("friendCount", count),
			zap.Error(err))

		return fmt.Errorf("failed to set friend count for user %d: %w", userID, err)
	}

	f.logger.Debug("Stored friend count in cache",
		zap.Int64("userID", userID),
		zap.Int("friendCount", count))

	return nil
}

// HasFriendCountChanged compares current friend count with cached value.
// Returns true if the count has changed or no cache entry exists.
func (f *FriendCountCache) HasFriendCountChanged(ctx context.Context, userID int64, currentCount int) (bool, error) {
	cachedCount, exists, err := f.GetFriendCount(ctx, userID)
	if err != nil {
		return true, err // Assume changed on error to be safe
	}

	if !exists {
		f.logger.Debug("No cached friend count, assuming changed",
			zap.Int64("userID", userID),
			zap.Int("currentCount", currentCount))

		return true, nil
	}

	changed := cachedCount != currentCount

	f.logger.Debug("Friend count comparison",
		zap.Int64("userID", userID),
		zap.Int("cachedCount", cachedCount),
		zap.Int("currentCount", currentCount),
		zap.Bool("changed", changed))

	return changed, nil
}

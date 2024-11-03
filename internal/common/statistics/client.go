package statistics

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/rueidis"
	"go.uber.org/zap"
)

const (
	// DailyStatsKeyPrefix forms the base key for daily statistics in Redis.
	DailyStatsKeyPrefix = "daily_statistics"

	// HourlyStatsKeyPrefix forms the base key for hourly statistics in Redis.
	HourlyStatsKeyPrefix = "hourly_statistics"

	// FieldUsersConfirmed tracks how many users were marked as inappropriate.
	FieldUsersConfirmed = "users_confirmed"
	// FieldUsersFlagged tracks how many users were flagged for review.
	FieldUsersFlagged = "users_flagged"
	// FieldUsersCleared tracks how many users were cleared during review.
	FieldUsersCleared = "users_cleared"
	// FieldBannedUsersPurged tracks how many banned users were removed.
	FieldBannedUsersPurged = "banned_users_purged"
	// FieldFlaggedUsersPurged tracks how many flagged users were removed.
	FieldFlaggedUsersPurged = "flagged_users_purged"
	// FieldClearedUsersPurged tracks how many cleared users were removed.
	FieldClearedUsersPurged = "cleared_users_purged"
)

// HourlyStat represents a single hour's statistics.
// The Hour field is used to order stats chronologically.
type HourlyStat struct {
	Hour      int `json:"hour"`
	Confirmed int `json:"confirmed"`
	Flagged   int `json:"flagged"`
	Cleared   int `json:"cleared"`
}

// HourlyStats represents a collection of hourly statistics.
type HourlyStats []HourlyStat

// Client handles Redis operations for storing and retrieving statistics.
type Client struct {
	Client rueidis.Client
	logger *zap.Logger
}

// NewClient creates a Client with the provided Redis connection and logger.
func NewClient(client rueidis.Client, logger *zap.Logger) *Client {
	return &Client{
		Client: client,
		logger: logger,
	}
}

// IncrementDailyStat atomically increases a daily statistic counter.
// The field parameter determines which counter to increment.
func (c *Client) IncrementDailyStat(ctx context.Context, field string, count int) error {
	// Build key for today's statistics
	date := time.Now().UTC().Format("2006-01-02")
	key := fmt.Sprintf("%s:%s", DailyStatsKeyPrefix, date)

	// Increment field using HINCRBY
	cmd := c.Client.B().Hincrby().Key(key).Field(field).Increment(int64(count)).Build()
	if err := c.Client.Do(ctx, cmd).Error(); err != nil {
		c.logger.Error("Failed to increment daily stat",
			zap.Error(err),
			zap.String("field", field),
			zap.Int("count", count))
		return err
	}

	return nil
}

// IncrementHourlyStat atomically increases an hourly statistic counter.
// The field parameter determines which counter to increment.
func (c *Client) IncrementHourlyStat(ctx context.Context, field string, count int) error {
	// Build key for current hour's statistics
	hour := time.Now().UTC().Format("2006-01-02:15")
	key := fmt.Sprintf("%s:%s", HourlyStatsKeyPrefix, hour)

	// Increment field using HINCRBY
	cmd := c.Client.B().Hincrby().Key(key).Field(field).Increment(int64(count)).Build()
	if err := c.Client.Do(ctx, cmd).Error(); err != nil {
		c.logger.Error("Failed to increment hourly stat",
			zap.Error(err),
			zap.String("field", field),
			zap.Int("count", count))
		return err
	}

	return nil
}

// GetHourlyStats retrieves statistics for the last 24 hours.
// It combines data from multiple Redis keys into a chronological list.
func (c *Client) GetHourlyStats(ctx context.Context) (HourlyStats, error) {
	stats := make(HourlyStats, 24)
	now := time.Now().UTC()

	// Collect stats for each of the last 24 hours
	for i := range stats {
		hour := now.Add(time.Duration(-i) * time.Hour)
		key := fmt.Sprintf("%s:%s", HourlyStatsKeyPrefix, hour.Format("2006-01-02:15"))

		// Get all fields for this hour using HGETALL
		cmd := c.Client.B().Hgetall().Key(key).Build()
		result, err := c.Client.Do(ctx, cmd).AsIntMap()
		if err != nil {
			c.logger.Error("Failed to get hourly stats",
				zap.Error(err),
				zap.String("key", key))
			return nil, err
		}

		// Store stats in chronological order
		stats[23-i] = HourlyStat{
			Hour:      hour.Hour(),
			Confirmed: int(result[FieldUsersConfirmed]),
			Flagged:   int(result[FieldUsersFlagged]),
			Cleared:   int(result[FieldUsersCleared]),
		}
	}

	return stats, nil
}

package statistics

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/rueidis"
	"go.uber.org/zap"
)

const (
	DBIndex = 1

	// Daily stats.
	DailyStatsKeyPrefix     = "daily_stats"
	FieldUsersConfirmed     = "users_confirmed"
	FieldUsersFlagged       = "users_flagged"
	FieldUsersCleared       = "users_cleared"
	FieldBannedUsersPurged  = "banned_users_purged"
	FieldFlaggedUsersPurged = "flagged_users_purged"
	FieldClearedUsersPurged = "cleared_users_purged"

	// Hourly stats.
	HourlyStatsKeyPrefix = "hourly_stats"
	HourlyStatConfirmed  = "confirmed"
	HourlyStatFlagged    = "flagged"
	HourlyStatCleared    = "cleared"
	HourlyStatsExpiry    = 24 * time.Hour
)

// HourlyStats represents hourly statistics data.
type HourlyStats struct {
	Timestamp time.Time
	Confirmed int64
	Flagged   int64
	Cleared   int64
}

// Statistics is a struct that contains a Redis client.
type Statistics struct {
	Client rueidis.Client
	logger *zap.Logger
}

// NewStatistics creates a new Statistics instance.
func NewStatistics(client rueidis.Client, logger *zap.Logger) *Statistics {
	return &Statistics{
		Client: client,
		logger: logger,
	}
}

// IncrementDailyStat increments a statistic for the current day.
func (s *Statistics) IncrementDailyStat(ctx context.Context, field string, amount int) error {
	date := time.Now().Format("2006-01-02")
	key := fmt.Sprintf("%s:%s", DailyStatsKeyPrefix, date)

	cmd := s.Client.B().Hincrby().Key(key).Field(field).Increment(int64(amount)).Build()
	return s.Client.Do(ctx, cmd).Error()
}

// IncrementHourlyStat increments a statistic for the current hour.
func (s *Statistics) IncrementHourlyStat(ctx context.Context, statType string) error {
	currentTime := time.Now()
	key := fmt.Sprintf("%s:%s:%s", HourlyStatsKeyPrefix, statType, currentTime.Format("2006-01-02-15"))

	// Increment the counter
	incrCmd := s.Client.B().Incr().Key(key).Build()
	if err := s.Client.Do(ctx, incrCmd).Error(); err != nil {
		return err
	}

	// Set expiry
	expireCmd := s.Client.B().Expire().Key(key).Seconds(int64(HourlyStatsExpiry.Seconds())).Build()
	return s.Client.Do(ctx, expireCmd).Error()
}

// GetHourlyStats retrieves statistics for the past 24 hours.
func (s *Statistics) GetHourlyStats(ctx context.Context) ([]HourlyStats, error) {
	currentTime := time.Now()
	stats := make([]HourlyStats, 24)

	// Initialize stats array with timestamps
	for i := range 24 {
		timestamp := currentTime.Add(time.Duration(-i) * time.Hour)
		stats[23-i] = HourlyStats{Timestamp: timestamp}
	}

	// Get stats for each hour
	for i, stat := range stats {
		hourFormat := stat.Timestamp.Format("2006-01-02-15")

		// Get values for each type
		confirmedKey := fmt.Sprintf("%s:%s:%s", HourlyStatsKeyPrefix, HourlyStatConfirmed, hourFormat)
		flaggedKey := fmt.Sprintf("%s:%s:%s", HourlyStatsKeyPrefix, HourlyStatFlagged, hourFormat)
		clearedKey := fmt.Sprintf("%s:%s:%s", HourlyStatsKeyPrefix, HourlyStatCleared, hourFormat)

		// Get confirmed count
		if result := s.Client.Do(ctx, s.Client.B().Get().Key(confirmedKey).Build()); result.Error() == nil {
			if val, err := result.AsInt64(); err == nil {
				stats[i].Confirmed = val
			}
		}

		// Get flagged count
		if result := s.Client.Do(ctx, s.Client.B().Get().Key(flaggedKey).Build()); result.Error() == nil {
			if val, err := result.AsInt64(); err == nil {
				stats[i].Flagged = val
			}
		}

		// Get cleared count
		if result := s.Client.Do(ctx, s.Client.B().Get().Key(clearedKey).Build()); result.Error() == nil {
			if val, err := result.AsInt64(); err == nil {
				stats[i].Cleared = val
			}
		}

		s.logger.Debug("Retrieved hourly stats",
			zap.String("hour", hourFormat),
			zap.Int64("confirmed", stats[i].Confirmed),
			zap.Int64("flagged", stats[i].Flagged),
			zap.Int64("cleared", stats[i].Cleared))
	}

	return stats, nil
}

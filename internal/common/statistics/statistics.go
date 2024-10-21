package statistics

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/rueidis"
)

const (
	StatisticsKey = "daily_statistics"
	DBIndex       = 1
)

// Statistics is a struct that contains a Redis client.
type Statistics struct {
	Client rueidis.Client
}

// NewStatistics creates a new Statistics instance.
func NewStatistics(client rueidis.Client) *Statistics {
	return &Statistics{Client: client}
}

// Increment increments a statistic in Redis.
func (s *Statistics) Increment(ctx context.Context, field string, amount int) error {
	date := time.Now().Format("2006-01-02")
	key := fmt.Sprintf("%s:%s", StatisticsKey, date)

	cmd := s.Client.B().Hincrby().Key(key).Field(field).Increment(int64(amount)).Build()
	return s.Client.Do(ctx, cmd).Error()
}

// IncrementUsersBanned increments the users_banned statistic.
func (s *Statistics) IncrementUsersBanned(ctx context.Context, amount int) error {
	return s.Increment(ctx, "users_banned", amount)
}

// IncrementUsersCleared increments the users_cleared statistic.
func (s *Statistics) IncrementUsersCleared(ctx context.Context, amount int) error {
	return s.Increment(ctx, "users_cleared", amount)
}

// IncrementUsersPending increments the users_pending statistic.
func (s *Statistics) IncrementUsersPending(ctx context.Context, amount int) error {
	return s.Increment(ctx, "users_pending", amount)
}

// IncrementUsersPurged increments the users_purged statistic.
func (s *Statistics) IncrementUsersPurged(ctx context.Context, amount int) error {
	return s.Increment(ctx, "users_purged", amount)
}

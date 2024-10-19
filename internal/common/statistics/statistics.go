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
func (s *Statistics) Increment(ctx context.Context, field string) error {
	date := time.Now().Format("2006-01-02")
	key := fmt.Sprintf("%s:%s", StatisticsKey, date)

	cmd := s.Client.B().Hincrby().Key(key).Field(field).Increment(1).Build()
	return s.Client.Do(ctx, cmd).Error()
}

// IncrementUsersBanned increments the users_banned statistic.
func (s *Statistics) IncrementUsersBanned(ctx context.Context) error {
	return s.Increment(ctx, "users_banned")
}

// IncrementUsersCleared increments the users_cleared statistic.
func (s *Statistics) IncrementUsersCleared(ctx context.Context) error {
	return s.Increment(ctx, "users_cleared")
}

// IncrementUsersPending increments the users_pending statistic.
func (s *Statistics) IncrementUsersPending(ctx context.Context) error {
	return s.Increment(ctx, "users_pending")
}

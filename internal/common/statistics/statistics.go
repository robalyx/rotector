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

	FieldUsersConfirmed     = "users_confirmed"
	FieldUsersFlagged       = "users_flagged"
	FieldUsersCleared       = "users_cleared"
	FieldBannedUsersPurged  = "banned_users_purged"
	FieldFlaggedUsersPurged = "flagged_users_purged"
	FieldClearedUsersPurged = "cleared_users_purged"
)

// Statistics is a struct that contains a Redis client.
type Statistics struct {
	Client rueidis.Client
}

// NewStatistics creates a new Statistics instance.
func NewStatistics(client rueidis.Client) *Statistics {
	return &Statistics{Client: client}
}

// IncrementField increments a statistic field in Redis.
func (s *Statistics) IncrementField(ctx context.Context, field string, amount int) error {
	date := time.Now().Format("2006-01-02")
	key := fmt.Sprintf("%s:%s", StatisticsKey, date)

	cmd := s.Client.B().Hincrby().Key(key).Field(field).Increment(int64(amount)).Build()
	return s.Client.Do(ctx, cmd).Error()
}

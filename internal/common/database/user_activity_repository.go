package database

import (
	"context"
	"time"

	"github.com/uptrace/bun"
	"go.uber.org/zap"
)

// ActivityType represents different kinds of user actions in the system.
//
//go:generate stringer -type=ActivityType -linecomment
type ActivityType int

const (
	// ActivityTypeAll matches any activity type in database queries.
	ActivityTypeAll ActivityType = iota // ALL
	// ActivityTypeViewed tracks when a moderator opens a user's profile.
	ActivityTypeViewed // VIEWED
	// ActivityTypeBanned tracks when a moderator confirms a user as inappropriate.
	ActivityTypeBanned // BANNED
	// ActivityTypeBannedCustom tracks bans with custom moderator-provided reasons.
	ActivityTypeBannedCustom // BANNED_CUSTOM
	// ActivityTypeCleared tracks when a moderator marks a user as appropriate.
	ActivityTypeCleared // CLEARED
	// ActivityTypeSkipped tracks when a moderator skips reviewing a user.
	ActivityTypeSkipped // SKIPPED
	// ActivityTypeRechecked tracks when a moderator requests an AI recheck.
	ActivityTypeRechecked // RECHECKED
)

// UserActivityLog stores information about moderator actions.
type UserActivityLog struct {
	UserID            uint64                 `bun:",notnull"`
	ReviewerID        uint64                 `bun:",notnull"`
	ActivityType      ActivityType           `bun:",notnull"`
	ActivityTimestamp time.Time              `bun:",notnull"`
	Details           map[string]interface{} `bun:"type:jsonb"`
}

// UserActivityRepository handles database operations for moderator action logs.
type UserActivityRepository struct {
	db     *bun.DB
	logger *zap.Logger
}

// NewUserActivityRepository creates a repository with database access for
// storing and retrieving moderator action logs.
func NewUserActivityRepository(db *bun.DB, logger *zap.Logger) *UserActivityRepository {
	return &UserActivityRepository{
		db:     db,
		logger: logger,
	}
}

// LogActivity stores a moderator action in the database.
func (r *UserActivityRepository) LogActivity(ctx context.Context, log *UserActivityLog) {
	_, err := r.db.NewInsert().Model(log).Exec(ctx)
	if err != nil {
		r.logger.Error("Failed to log user activity",
			zap.Error(err),
			zap.Uint64("userID", log.UserID),
			zap.Uint64("reviewerID", log.ReviewerID),
			zap.String("activityType", log.ActivityType.String()))
		return
	}

	r.logger.Info("Logged user activity",
		zap.Uint64("userID", log.UserID),
		zap.Uint64("reviewerID", log.ReviewerID),
		zap.String("activityType", log.ActivityType.String()))
}

// GetLogs retrieves activity logs based on filter criteria:
// - User ID filters logs for a specific user
// - Reviewer ID filters logs by a specific moderator
// - Activity type filters by action type
// - Date range filters by when the action occurred
// - Page and limit control result pagination.
func (r *UserActivityRepository) GetLogs(
	ctx context.Context,
	userID uint64,
	reviewerID uint64,
	activityType ActivityType,
	startDate time.Time,
	endDate time.Time,
	page int,
	limit int,
) ([]*UserActivityLog, int, error) {
	var logs []*UserActivityLog

	// Build base query conditions
	baseQuery := func(q *bun.SelectQuery) *bun.SelectQuery {
		if userID != 0 {
			q = q.Where("user_id = ?", userID)
		}
		if reviewerID != 0 {
			q = q.Where("reviewer_id = ?", reviewerID)
		}
		if activityType != ActivityTypeAll {
			q = q.Where("activity_type = ?", activityType)
		}
		if !startDate.IsZero() && !endDate.IsZero() {
			q = q.Where("activity_timestamp BETWEEN ? AND ?", startDate, endDate)
		}
		return q
	}

	// Get total count
	total, err := baseQuery(
		r.db.NewSelect().Model((*UserActivityLog)(nil)),
	).Count(ctx)
	if err != nil {
		r.logger.Error("Failed to get total log count", zap.Error(err))
		return nil, 0, err
	}

	// Get paginated results
	err = baseQuery(
		r.db.NewSelect().Model(&logs),
	).Order("activity_timestamp DESC").
		Limit(limit).
		Offset(page * limit).
		Scan(ctx)
	if err != nil {
		r.logger.Error("Failed to get logs", zap.Error(err))
		return nil, 0, err
	}

	r.logger.Info("Retrieved logs",
		zap.Int("total", total),
		zap.Int("page", page),
		zap.Int("limit", limit))

	return logs, total, nil
}

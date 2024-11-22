package models

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

	// ActivityTypeUserViewed tracks when a moderator opens a user's profile.
	ActivityTypeUserViewed // USER_VIEWED
	// ActivityTypeUserConfirmed tracks when a moderator confirms a user as inappropriate.
	ActivityTypeUserConfirmed // USER_CONFIRMED
	// ActivityTypeUserConfirmedCustom tracks bans with custom moderator-provided reasons.
	ActivityTypeUserConfirmedCustom // USER_CONFIRMED_CUSTOM
	// ActivityTypeUserCleared tracks when a moderator marks a user as appropriate.
	ActivityTypeUserCleared // USER_CLEARED
	// ActivityTypeUserSkipped tracks when a moderator skips reviewing a user.
	ActivityTypeUserSkipped // USER_SKIPPED
	// ActivityTypeUserRechecked tracks when a moderator requests an AI recheck.
	ActivityTypeUserRechecked // USER_RECHECKED
	// ActivityTypeUserTrainingUpvote tracks when a moderator upvotes a user in training mode.
	ActivityTypeUserTrainingUpvote // USER_TRAINING_UPVOTE
	// ActivityTypeUserTrainingDownvote tracks when a moderator downvotes a user in training mode.
	ActivityTypeUserTrainingDownvote // USER_TRAINING_DOWNVOTE

	// ActivityTypeGroupViewed tracks when a moderator opens a group's profile.
	ActivityTypeGroupViewed // GROUP_VIEWED
	// ActivityTypeGroupConfirmed tracks when a moderator confirms a group as inappropriate.
	ActivityTypeGroupConfirmed // GROUP_CONFIRMED
	// ActivityTypeGroupConfirmedCustom tracks bans with custom moderator-provided reasons.
	ActivityTypeGroupConfirmedCustom // GROUP_CONFIRMED_CUSTOM
	// ActivityTypeGroupCleared tracks when a moderator marks a group as appropriate.
	ActivityTypeGroupCleared // GROUP_CLEARED
	// ActivityTypeGroupSkipped tracks when a moderator skips reviewing a group.
	ActivityTypeGroupSkipped // GROUP_SKIPPED
	// ActivityTypeGroupTrainingUpvote tracks when a moderator upvotes a group in training mode.
	ActivityTypeGroupTrainingUpvote // GROUP_TRAINING_UPVOTE
	// ActivityTypeGroupTrainingDownvote tracks when a moderator downvotes a group in training mode.
	ActivityTypeGroupTrainingDownvote // GROUP_TRAINING_DOWNVOTE
)

// ActivityTarget identifies the target of an activity log entry.
// Only one of UserID or GroupID should be set.
type ActivityTarget struct {
	UserID  uint64 `bun:",nullzero"` // Set to 0 for group activities
	GroupID uint64 `bun:",nullzero"` // Set to 0 for user activities
}

// ActivityFilter is used to provide a filter criteria for retrieving activity logs.
type ActivityFilter struct {
	UserID       uint64
	GroupID      uint64
	ReviewerID   uint64
	ActivityType ActivityType
	StartDate    time.Time
	EndDate      time.Time
}

// UserActivityLog stores information about moderator actions.
type UserActivityLog struct {
	ActivityTarget
	ReviewerID        uint64                 `bun:",notnull"`
	ActivityType      ActivityType           `bun:",notnull"`
	ActivityTimestamp time.Time              `bun:",notnull"`
	Details           map[string]interface{} `bun:"type:jsonb"`
}

// UserActivityModel handles database operations for moderator action logs.
type UserActivityModel struct {
	db     *bun.DB
	logger *zap.Logger
}

// NewUserActivity creates a repository with database access for
// storing and retrieving moderator action logs.
func NewUserActivity(db *bun.DB, logger *zap.Logger) *UserActivityModel {
	return &UserActivityModel{
		db:     db,
		logger: logger,
	}
}

// LogActivity stores a moderator action in the database.
func (r *UserActivityModel) LogActivity(ctx context.Context, log *UserActivityLog) {
	// Validate that only one target type is set
	if (log.UserID != 0 && log.GroupID != 0) || (log.UserID == 0 && log.GroupID == 0) {
		r.logger.Error("Invalid activity log target",
			zap.Uint64("userID", log.UserID),
			zap.Uint64("groupID", log.GroupID))
		return
	}

	_, err := r.db.NewInsert().Model(log).Exec(ctx)
	if err != nil {
		r.logger.Error("Failed to log activity",
			zap.Error(err),
			zap.Uint64("userID", log.UserID),
			zap.Uint64("groupID", log.GroupID),
			zap.Uint64("reviewerID", log.ReviewerID),
			zap.String("activityType", log.ActivityType.String()))
		return
	}

	r.logger.Debug("Logged activity",
		zap.Uint64("userID", log.UserID),
		zap.Uint64("groupID", log.GroupID),
		zap.Uint64("reviewerID", log.ReviewerID),
		zap.String("activityType", log.ActivityType.String()))
}

// GetLogs retrieves activity logs based on filter criteria.
func (r *UserActivityModel) GetLogs(ctx context.Context, filter ActivityFilter, page, limit int) ([]*UserActivityLog, int, error) {
	var logs []*UserActivityLog

	// Build base query conditions
	baseQuery := func(q *bun.SelectQuery) *bun.SelectQuery {
		if filter.UserID != 0 {
			q = q.Where("user_id = ?", filter.UserID)
		}
		if filter.GroupID != 0 {
			q = q.Where("group_id = ?", filter.GroupID)
		}
		if filter.ReviewerID != 0 {
			q = q.Where("reviewer_id = ?", filter.ReviewerID)
		}
		if filter.ActivityType != ActivityTypeAll {
			q = q.Where("activity_type = ?", filter.ActivityType)
		}
		if !filter.StartDate.IsZero() && !filter.EndDate.IsZero() {
			q = q.Where("activity_timestamp BETWEEN ? AND ?", filter.StartDate, filter.EndDate)
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

	r.logger.Debug("Retrieved logs",
		zap.Int("total", total),
		zap.Int("page", page),
		zap.Int("limit", limit))

	return logs, total, nil
}

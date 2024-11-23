package models

import (
	"context"
	"errors"
	"time"

	"github.com/uptrace/bun"
	"go.uber.org/zap"
)

// Add at the top with other constants/types.
var ErrNoLogsFound = errors.New("no logs found")

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

// LogCursor represents a pagination cursor for activity logs.
type LogCursor struct {
	Timestamp time.Time
	Sequence  int64
}

// UserActivityLog stores information about moderator actions.
type UserActivityLog struct {
	Sequence int64 `bun:",pk,autoincrement"`
	ActivityTarget
	ReviewerID        uint64                 `bun:",notnull"`
	ActivityType      ActivityType           `bun:",notnull"`
	ActivityTimestamp time.Time              `bun:",notnull,pk"`
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
func (r *UserActivityModel) GetLogs(ctx context.Context, filter ActivityFilter, cursor *LogCursor, limit int) ([]*UserActivityLog, *LogCursor, error) {
	var logs []*UserActivityLog

	// Build base query conditions
	query := r.db.NewSelect().Model(&logs)

	if filter.UserID != 0 {
		query = query.Where("user_id = ?", filter.UserID)
	}
	if filter.GroupID != 0 {
		query = query.Where("group_id = ?", filter.GroupID)
	}
	if filter.ReviewerID != 0 {
		query = query.Where("reviewer_id = ?", filter.ReviewerID)
	}
	if filter.ActivityType != ActivityTypeAll {
		query = query.Where("activity_type = ?", filter.ActivityType)
	}
	if !filter.StartDate.IsZero() && !filter.EndDate.IsZero() {
		query = query.Where("activity_timestamp BETWEEN ? AND ?", filter.StartDate, filter.EndDate)
	}

	// Apply cursor conditions if cursor exists
	if cursor != nil {
		query = query.Where("(activity_timestamp, sequence) <= (?, ?)", cursor.Timestamp, cursor.Sequence)
	}

	// Order by timestamp and sequence for stable pagination
	query = query.Order("activity_timestamp DESC", "sequence DESC").
		Limit(limit + 1) // Get one extra to determine if there are more results

	err := query.Scan(ctx)
	if err != nil {
		r.logger.Error("Failed to get logs", zap.Error(err))
		return nil, nil, err
	}

	var nextCursor *LogCursor
	if len(logs) > limit {
		// If we got more results than the limit, the last item becomes our next cursor
		nextCursor = &LogCursor{
			Timestamp: logs[limit].ActivityTimestamp,
			Sequence:  logs[limit].Sequence,
		}
		logs = logs[:limit] // Remove the extra item
	}

	return logs, nextCursor, nil
}

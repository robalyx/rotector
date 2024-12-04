package models

import (
	"context"
	"errors"

	"github.com/rotector/rotector/internal/common/storage/database/types"
	"github.com/uptrace/bun"
	"go.uber.org/zap"
)

// Add at the top with other constants/types.
var ErrNoLogsFound = errors.New("no logs found")

// ActivityModel handles database operations for moderator action logs.
type ActivityModel struct {
	db     *bun.DB
	logger *zap.Logger
}

// NewUserActivity creates a repository with database access for
// storing and retrieving moderator action logs.
func NewUserActivity(db *bun.DB, logger *zap.Logger) *ActivityModel {
	return &ActivityModel{
		db:     db,
		logger: logger,
	}
}

// Log stores a moderator action in the database.
func (r *ActivityModel) Log(ctx context.Context, log *types.UserActivityLog) {
	// Validate that only one target type is set
	if (log.ActivityTarget.UserID != 0 && log.ActivityTarget.GroupID != 0) || (log.ActivityTarget.UserID == 0 && log.ActivityTarget.GroupID == 0) {
		r.logger.Error("Invalid activity log target",
			zap.Uint64("userID", log.ActivityTarget.UserID),
			zap.Uint64("groupID", log.ActivityTarget.GroupID))
		return
	}

	_, err := r.db.NewInsert().Model(log).Exec(ctx)
	if err != nil {
		r.logger.Error("Failed to log activity",
			zap.Error(err),
			zap.Uint64("userID", log.ActivityTarget.UserID),
			zap.Uint64("groupID", log.ActivityTarget.GroupID),
			zap.Uint64("reviewerID", log.ReviewerID),
			zap.String("activityType", log.ActivityType.String()))
		return
	}

	r.logger.Debug("Logged activity",
		zap.Uint64("userID", log.ActivityTarget.UserID),
		zap.Uint64("groupID", log.ActivityTarget.GroupID),
		zap.Uint64("reviewerID", log.ReviewerID),
		zap.String("activityType", log.ActivityType.String()))
}

// GetLogs retrieves activity logs based on filter criteria.
func (r *ActivityModel) GetLogs(ctx context.Context, filter types.ActivityFilter, cursor *types.LogCursor, limit int) ([]*types.UserActivityLog, *types.LogCursor, error) {
	var logs []*types.UserActivityLog

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
	if filter.ActivityType != types.ActivityTypeAll {
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

	var nextCursor *types.LogCursor
	if len(logs) > limit {
		// If we got more results than the limit, the last item becomes our next cursor
		nextCursor = &types.LogCursor{
			Timestamp: logs[limit].ActivityTimestamp,
			Sequence:  logs[limit].Sequence,
		}
		logs = logs[:limit] // Remove the extra item
	}

	return logs, nextCursor, nil
}

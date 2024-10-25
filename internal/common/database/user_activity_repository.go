package database

import (
	"time"

	"github.com/go-pg/pg/v10"
	"github.com/rotector/rotector/internal/bot/constants"
	"go.uber.org/zap"
)

// ActivityType represents the type of user activity.
type ActivityType string

const (
	ActivityTypeAll          ActivityType = "ALL"
	ActivityTypeReviewed     ActivityType = "REVIEWED"
	ActivityTypeBanned       ActivityType = "BANNED"
	ActivityTypeBannedCustom ActivityType = "BANNED_CUSTOM"
	ActivityTypeCleared      ActivityType = "CLEARED"
	ActivityTypeSkipped      ActivityType = "SKIPPED"
)

// UserActivityLog represents a log entry for user activity.
type UserActivityLog struct {
	UserID            uint64                 `pg:"user_id,notnull"`
	ReviewerID        uint64                 `pg:"reviewer_id,notnull"`
	ActivityType      ActivityType           `pg:"activity_type,notnull"`
	ActivityTimestamp time.Time              `pg:"activity_timestamp,notnull"`
	Details           map[string]interface{} `pg:"details,type:jsonb"`
}

// UserActivityRepository handles user activity logging operations.
type UserActivityRepository struct {
	db     *pg.DB
	logger *zap.Logger
}

// NewUserActivityRepository creates a new UserActivityRepository instance.
func NewUserActivityRepository(db *pg.DB, logger *zap.Logger) *UserActivityRepository {
	return &UserActivityRepository{
		db:     db,
		logger: logger,
	}
}

// LogActivity logs a user activity.
func (r *UserActivityRepository) LogActivity(log *UserActivityLog) {
	if _, err := r.db.Model(log).Insert(); err != nil {
		r.logger.Error("Failed to log user activity", zap.Error(err))
	}
}

// GetLogs retrieves logs based on the given parameters.
func (r *UserActivityRepository) GetLogs(queryType string, queryID uint64, activityTypeFilter string, page, perPage int) ([]*UserActivityLog, int, error) {
	var logs []*UserActivityLog

	query := r.db.Model(&UserActivityLog{})

	if queryType == constants.LogsQueryUserIDOption {
		query = query.Where("user_id = ?", queryID)
	} else if queryType == constants.LogsQueryReviewerIDOption {
		query = query.Where("reviewer_id = ?", queryID)
	}

	if activityTypeFilter != "" && activityTypeFilter != string(ActivityTypeAll) {
		query = query.Where("activity_type = ?", activityTypeFilter)
	}

	totalLogs, err := query.Count()
	if err != nil {
		return nil, 0, err
	}

	err = query.
		Order("activity_timestamp DESC").
		Offset(page * perPage).
		Limit(perPage).
		Select(&logs)
	if err != nil {
		return nil, 0, err
	}

	r.logger.Info("Retrieved logs",
		zap.String("query_type", queryType),
		zap.Uint64("query_id", queryID),
		zap.String("activity_type_filter", activityTypeFilter),
		zap.Int("total_logs", totalLogs),
		zap.Int("page", page),
		zap.Int("per_page", perPage),
	)

	return logs, totalLogs, nil
}

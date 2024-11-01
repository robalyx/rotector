package database

import (
	"time"

	"github.com/go-pg/pg/v10"
	"go.uber.org/zap"
)

// ActivityType represents the type of user activity.
type ActivityType int

const (
	ActivityTypeAll ActivityType = iota
	ActivityTypeViewed
	ActivityTypeBanned
	ActivityTypeBannedCustom
	ActivityTypeCleared
	ActivityTypeSkipped
	ActivityTypeRechecked
)

// String returns the string representation of an ActivityType.
func (a ActivityType) String() string {
	return [...]string{"ALL", "VIEWED", "BANNED", "BANNED_CUSTOM", "CLEARED", "SKIPPED", "RECHECKED"}[a]
}

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
func (r *UserActivityRepository) GetLogs(userID, reviewerID uint64, activityTypeFilter ActivityType, startDate, endDate time.Time, page, perPage int) ([]*UserActivityLog, int, error) {
	var logs []*UserActivityLog

	query := r.db.Model(&UserActivityLog{})

	if userID != 0 {
		query = query.Where("user_id = ?", userID)
	}

	if reviewerID != 0 {
		query = query.Where("reviewer_id = ?", reviewerID)
	}

	if !startDate.IsZero() && !endDate.IsZero() {
		query = query.Where("activity_timestamp BETWEEN ? AND ?", startDate, endDate)
	}

	if activityTypeFilter != ActivityTypeAll {
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
		zap.Uint64("user_id", userID),
		zap.Uint64("reviewer_id", reviewerID),
		zap.String("activity_type_filter", activityTypeFilter.String()),
		zap.Time("start_date", startDate),
		zap.Time("end_date", endDate),
		zap.Int("total_logs", totalLogs),
		zap.Int("page", page),
		zap.Int("per_page", perPage),
	)

	return logs, totalLogs, nil
}

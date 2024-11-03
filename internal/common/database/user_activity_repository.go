package database

import (
	"time"

	"github.com/go-pg/pg/v10"
	"go.uber.org/zap"
)

// ActivityType represents different kinds of user actions in the system.
type ActivityType int

const (
	// ActivityTypeAll matches any activity type in database queries.
	ActivityTypeAll = iota
	// ActivityTypeViewed tracks when a moderator opens a user's profile.
	ActivityTypeViewed
	// ActivityTypeBanned tracks when a moderator confirms a user as inappropriate.
	ActivityTypeBanned
	// ActivityTypeBannedCustom tracks bans with custom moderator-provided reasons.
	ActivityTypeBannedCustom
	// ActivityTypeCleared tracks when a moderator marks a user as appropriate.
	ActivityTypeCleared
	// ActivityTypeSkipped tracks when a moderator skips reviewing a user.
	ActivityTypeSkipped
	// ActivityTypeRechecked tracks when a moderator requests an AI recheck.
	ActivityTypeRechecked
)

// String returns the string representation of an ActivityType.
func (a ActivityType) String() string {
	return [...]string{"ALL", "VIEWED", "BANNED", "BANNED_CUSTOM", "CLEARED", "SKIPPED", "RECHECKED"}[a]
}

// UserActivityLog stores information about moderator actions.
type UserActivityLog struct {
	UserID            uint64                 `pg:"user_id,notnull"`
	ReviewerID        uint64                 `pg:"reviewer_id,notnull"`
	ActivityType      ActivityType           `pg:"activity_type,notnull"`
	ActivityTimestamp time.Time              `pg:"activity_timestamp,notnull"`
	Details           map[string]interface{} `pg:"details,type:jsonb"`
}

// UserActivityRepository handles database operations for moderator action logs.
type UserActivityRepository struct {
	db     *pg.DB
	logger *zap.Logger
}

// NewUserActivityRepository creates a repository with database access for
// storing and retrieving moderator action logs.
func NewUserActivityRepository(db *pg.DB, logger *zap.Logger) *UserActivityRepository {
	return &UserActivityRepository{
		db:     db,
		logger: logger,
	}
}

// LogActivity stores a moderator action in the database.
func (r *UserActivityRepository) LogActivity(log *UserActivityLog) {
	_, err := r.db.Model(log).Insert()
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
	userID uint64,
	reviewerID uint64,
	activityType ActivityType,
	startDate time.Time,
	endDate time.Time,
	page int,
	limit int,
) ([]*UserActivityLog, int, error) {
	var logs []*UserActivityLog
	query := r.db.Model(&logs)

	// Apply filters if provided
	if userID != 0 {
		query = query.Where("user_id = ?", userID)
	}
	if reviewerID != 0 {
		query = query.Where("reviewer_id = ?", reviewerID)
	}
	if activityType != ActivityTypeAll {
		query = query.Where("activity_type = ?", activityType)
	}
	if !startDate.IsZero() && !endDate.IsZero() {
		query = query.Where("activity_timestamp BETWEEN ? AND ?", startDate, endDate)
	}

	// Get total count before pagination
	total, err := query.Clone().Count()
	if err != nil {
		r.logger.Error("Failed to get total log count", zap.Error(err))
		return nil, 0, err
	}

	// Apply pagination and fetch results
	err = query.
		Order("activity_timestamp DESC").
		Limit(limit).
		Offset(page * limit).
		Select()
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

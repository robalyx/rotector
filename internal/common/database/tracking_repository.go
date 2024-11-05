package database

import (
	"time"

	"github.com/go-pg/pg/v10"
	"go.uber.org/zap"
)

// TrackingRepository handles database operations for monitoring affiliations
// between users and groups.
type TrackingRepository struct {
	db     *pg.DB
	logger *zap.Logger
}

// NewTrackingRepository creates a TrackingRepository for tracking group members.
func NewTrackingRepository(db *pg.DB, logger *zap.Logger) *TrackingRepository {
	return &TrackingRepository{
		db:     db,
		logger: logger,
	}
}

// AddUserToGroupTracking adds a confirmed user to a group's tracking list.
// The LastAppended field is updated to help with cleanup of old tracking data.
func (r *TrackingRepository) AddUserToGroupTracking(groupID, userID uint64) error {
	_, err := r.db.Model(&GroupMemberTracking{
		GroupID:        groupID,
		ConfirmedUsers: []uint64{userID},
		LastAppended:   time.Now(),
	}).OnConflict("(group_id) DO UPDATE").
		Set("confirmed_users = array_append(EXCLUDED.confirmed_users, ?)", userID).
		Set("last_appended = EXCLUDED.last_appended").
		Insert()
	if err != nil {
		r.logger.Error("Failed to add user to group tracking",
			zap.Error(err),
			zap.Uint64("groupID", groupID),
			zap.Uint64("userID", userID))
		return err
	}

	return nil
}

// PurgeOldTrackings removes tracking entries that haven't been updated recently.
// This helps maintain database size by removing stale tracking data.
func (r *TrackingRepository) PurgeOldTrackings(cutoffDate time.Time) (int, error) {
	// Remove old group trackings
	groupRes, err := r.db.Model((*GroupMemberTracking)(nil)).
		Where("last_appended < ?", cutoffDate).
		Delete()
	if err != nil {
		r.logger.Error("Failed to purge old group trackings", zap.Error(err))
		return 0, err
	}

	rowsAffected := groupRes.RowsAffected()
	return rowsAffected, nil
}

// GetAndRemoveQualifiedGroupTrackings finds groups with enough confirmed users
// to warrant flagging. Groups are removed from tracking after being returned.
func (r *TrackingRepository) GetAndRemoveQualifiedGroupTrackings(minConfirmedUsers int) (map[uint64]int, error) {
	var trackings []GroupMemberTracking

	// Find groups with enough confirmed users
	err := r.db.Model(&trackings).
		Where("array_length(confirmed_users, 1) >= ?", minConfirmedUsers).
		Select()
	if err != nil {
		r.logger.Error("Failed to get qualified group trackings", zap.Error(err))
		return nil, err
	}

	// Extract group IDs for deletion
	groupIDs := make([]uint64, len(trackings))
	for i, tracking := range trackings {
		groupIDs[i] = tracking.GroupID
	}

	// Remove found groups from tracking
	if len(groupIDs) > 0 {
		_, err = r.db.Model((*GroupMemberTracking)(nil)).
			Where("group_id IN (?)", pg.In(groupIDs)).
			Delete()
		if err != nil {
			r.logger.Error("Failed to delete group trackings", zap.Error(err))
			return nil, err
		}
	}

	// Map group IDs to their confirmed user counts
	result := make(map[uint64]int)
	for _, tracking := range trackings {
		result[tracking.GroupID] = len(tracking.ConfirmedUsers)
	}

	return result, nil
}

package database

import (
	"context"
	"time"

	"github.com/uptrace/bun"
	"go.uber.org/zap"
)

// GroupMemberTracking monitors confirmed users within groups.
// The LastAppended field helps determine when to purge old tracking data.
type GroupMemberTracking struct {
	GroupID        uint64    `bun:",pk"`
	ConfirmedUsers []uint64  `bun:"type:bigint[]"`
	LastAppended   time.Time `bun:",notnull"`
}

// TrackingRepository handles database operations for monitoring affiliations
// between users and groups.
type TrackingRepository struct {
	db     *bun.DB
	logger *zap.Logger
}

// NewTrackingRepository creates a TrackingRepository for tracking group members.
func NewTrackingRepository(db *bun.DB, logger *zap.Logger) *TrackingRepository {
	return &TrackingRepository{
		db:     db,
		logger: logger,
	}
}

// AddUserToGroupTracking adds a user ID to a group's tracking list.
// If the group doesn't exist, it creates a new tracking entry.
func (r *TrackingRepository) AddUserToGroupTracking(ctx context.Context, groupID, userID uint64) error {
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Check if group is already confirmed
		exists, err := tx.NewSelect().Model((*ConfirmedGroup)(nil)).
			Where("id = ?", groupID).
			Exists(ctx)
		if err != nil {
			r.logger.Error("Failed to check confirmed group",
				zap.Error(err),
				zap.Uint64("groupID", groupID))
			return err
		}
		if exists {
			r.logger.Debug("Skipping tracking for confirmed group",
				zap.Uint64("groupID", groupID),
				zap.Uint64("userID", userID))
			return nil
		}

		// Add user to group tracking
		_, err = r.db.NewInsert().Model(&GroupMemberTracking{
			GroupID:        groupID,
			ConfirmedUsers: []uint64{userID},
			LastAppended:   time.Now(),
		}).On("CONFLICT (group_id) DO UPDATE").
			Set("confirmed_users = array_append(array_remove(group_member_tracking.confirmed_users, ?), ?)", userID, userID).
			Set("last_appended = EXCLUDED.last_appended").
			Exec(ctx)
		if err != nil {
			r.logger.Error("Failed to add user to group tracking",
				zap.Error(err),
				zap.Uint64("groupID", groupID),
				zap.Uint64("userID", userID))
			return err
		}

		return nil
	})
}

// PurgeOldTrackings removes tracking entries that haven't been updated recently.
func (r *TrackingRepository) PurgeOldTrackings(ctx context.Context, cutoffDate time.Time) (int, error) {
	result, err := r.db.NewDelete().Model((*GroupMemberTracking)(nil)).
		Where("last_appended < ?", cutoffDate).
		Exec(ctx)
	if err != nil {
		r.logger.Error("Failed to purge old group trackings", zap.Error(err))
		return 0, err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		r.logger.Error("Failed to get rows affected", zap.Error(err))
		return 0, err
	}

	return int(rowsAffected), nil
}

// GetAndRemoveQualifiedGroupTrackings finds groups with enough confirmed users.
func (r *TrackingRepository) GetAndRemoveQualifiedGroupTrackings(ctx context.Context, minConfirmedUsers int) (map[uint64]int, error) {
	var trackings []GroupMemberTracking

	// Find groups with enough confirmed users
	err := r.db.NewSelect().Model(&trackings).
		Where("array_length(confirmed_users, 1) >= ?", minConfirmedUsers).
		Scan(ctx)
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
		_, err = r.db.NewDelete().Model((*GroupMemberTracking)(nil)).
			Where("group_id IN (?)", bun.In(groupIDs)).
			Exec(ctx)
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

package models

import (
	"context"
	"time"

	"github.com/uptrace/bun"
	"go.uber.org/zap"
)

// GroupMemberTracking monitors confirmed users within groups.
// The LastAppended field helps determine when to purge old tracking data.
type GroupMemberTracking struct {
	GroupID      uint64    `bun:",pk"`
	FlaggedUsers []uint64  `bun:"type:bigint[]"`
	LastAppended time.Time `bun:",notnull"`
}

// TrackingModel handles database operations for monitoring affiliations
// between users and groups.
type TrackingModel struct {
	db     *bun.DB
	logger *zap.Logger
}

// NewTracking creates a TrackingModel for tracking group members.
func NewTracking(db *bun.DB, logger *zap.Logger) *TrackingModel {
	return &TrackingModel{
		db:     db,
		logger: logger,
	}
}

// AddUserToGroupTracking adds a user ID to a group's tracking list.
// If the group exists in confirmed_groups or flagged_groups, it adds the user to their lists.
// Otherwise, it creates a new tracking entry.
func (r *TrackingModel) AddUserToGroupTracking(ctx context.Context, groupID, userID uint64) error {
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Check if group is in confirmed_groups
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
			// Add user to confirmed group's flagged users list
			_, err = tx.NewUpdate().Model(&ConfirmedGroup{}).
				Set("flagged_users = array_append(array_remove(flagged_users, ?), ?)", userID, userID).
				Where("id = ?", groupID).
				Exec(ctx)
			if err != nil {
				r.logger.Error("Failed to update confirmed group's flagged users",
					zap.Error(err),
					zap.Uint64("groupID", groupID),
					zap.Uint64("userID", userID))
				return err
			}
			return nil
		}

		// Check if group is in flagged_groups
		exists, err = tx.NewSelect().Model((*FlaggedGroup)(nil)).
			Where("id = ?", groupID).
			Exists(ctx)
		if err != nil {
			r.logger.Error("Failed to check flagged group",
				zap.Error(err),
				zap.Uint64("groupID", groupID))
			return err
		}

		if exists {
			// Add user to flagged group's flagged users list
			_, err = tx.NewUpdate().Model(&FlaggedGroup{}).
				Set("flagged_users = array_append(array_remove(flagged_users, ?), ?)", userID, userID).
				Where("id = ?", groupID).
				Exec(ctx)
			if err != nil {
				r.logger.Error("Failed to update flagged group's flagged users",
					zap.Error(err),
					zap.Uint64("groupID", groupID),
					zap.Uint64("userID", userID))
				return err
			}
			return nil
		}

		// If group is not in either table, create new tracking entry
		_, err = tx.NewInsert().Model(&GroupMemberTracking{
			GroupID:      groupID,
			FlaggedUsers: []uint64{userID},
			LastAppended: time.Now(),
		}).On("CONFLICT (group_id) DO UPDATE").
			Set("flagged_users = array_append(array_remove(group_member_tracking.flagged_users, ?), ?)", userID, userID).
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
func (r *TrackingModel) PurgeOldTrackings(ctx context.Context, cutoffDate time.Time) (int, error) {
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

// GetAndRemoveQualifiedGroupTrackings finds groups with enough flagged users.
// GetAndRemoveQualifiedGroupTrackings returns a map of group IDs to their flagged user IDs.
func (r *TrackingModel) GetAndRemoveQualifiedGroupTrackings(ctx context.Context, minFlaggedUsers int) (map[uint64][]uint64, error) {
	var trackings []GroupMemberTracking

	// Find groups with enough flagged users
	err := r.db.NewSelect().Model(&trackings).
		Where("array_length(flagged_users, 1) >= ?", minFlaggedUsers).
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

	// Map group IDs to their flagged user lists
	result := make(map[uint64][]uint64)
	for _, tracking := range trackings {
		result[tracking.GroupID] = tracking.FlaggedUsers
	}

	return result, nil
}

package models

import (
	"context"
	"sort"
	"time"

	"github.com/rotector/rotector/internal/common/storage/database/types"
	"github.com/uptrace/bun"
	"go.uber.org/zap"
)

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

// AddUsersToGroupsTracking adds multiple users to multiple groups' tracking lists.
func (r *TrackingModel) AddUsersToGroupsTracking(ctx context.Context, groupToUsers map[uint64][]uint64) error {
	// Create tracking entries for bulk insert
	trackings := make([]types.GroupMemberTracking, 0, len(groupToUsers))
	now := time.Now()

	for groupID, userIDs := range groupToUsers {
		trackings = append(trackings, types.GroupMemberTracking{
			GroupID:      groupID,
			FlaggedUsers: userIDs,
			LastAppended: now,
			IsFlagged:    false,
		})
	}

	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Lock the groups in a consistent order to prevent deadlocks
		groupIDs := make([]uint64, 0, len(groupToUsers))
		for groupID := range groupToUsers {
			groupIDs = append(groupIDs, groupID)
		}
		sort.Slice(groupIDs, func(i, j int) bool { return groupIDs[i] < groupIDs[j] })

		// Lock the rows we're going to update
		var existing []types.GroupMemberTracking
		err := tx.NewSelect().
			Model(&existing).
			Where("group_id IN (?)", bun.In(groupIDs)).
			For("UPDATE").
			Order("group_id").
			Scan(ctx)
		if err != nil {
			return err
		}

		// Perform bulk insert with upsert
		_, err = tx.NewInsert().
			Model(&trackings).
			On("CONFLICT (group_id) DO UPDATE").
			Set("flagged_users = ARRAY(SELECT DISTINCT unnest(EXCLUDED.flagged_users || group_member_tracking.flagged_users))").
			Set("last_appended = EXCLUDED.last_appended").
			Set("is_flagged = group_member_tracking.is_flagged").
			Exec(ctx)
		if err != nil {
			r.logger.Error("Failed to add users to groups tracking",
				zap.Error(err),
				zap.Int("groupCount", len(groupToUsers)))
			return err
		}

		return nil
	})
	if err != nil {
		return err
	}

	r.logger.Debug("Successfully processed group tracking updates",
		zap.Int("groupCount", len(groupToUsers)))

	return nil
}

// PurgeOldTrackings removes tracking entries that haven't been updated recently.
func (r *TrackingModel) PurgeOldTrackings(ctx context.Context, cutoffDate time.Time) (int, error) {
	result, err := r.db.NewDelete().Model((*types.GroupMemberTracking)(nil)).
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

// GetGroupTrackingsToCheck finds groups that haven't been checked recently.
func (r *TrackingModel) GetGroupTrackingsToCheck(ctx context.Context, batchSize int) (map[uint64][]uint64, error) {
	result := make(map[uint64][]uint64)

	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var trackings []types.GroupMemberTracking

		// Find groups that haven't been checked in the last 10 minutes
		err := tx.NewSelect().Model(&trackings).
			Where("is_flagged = false").
			Where("last_checked < ?", time.Now().Add(-10*time.Minute)).
			Order("last_checked ASC").
			Limit(batchSize).
			For("UPDATE").
			Scan(ctx)
		if err != nil {
			r.logger.Error("Failed to get group trackings to check", zap.Error(err))
			return err
		}

		// Update last_checked timestamp for these groups
		groupIDs := make([]uint64, len(trackings))
		for i, tracking := range trackings {
			groupIDs[i] = tracking.GroupID
		}

		if len(groupIDs) > 0 {
			_, err = tx.NewUpdate().Model((*types.GroupMemberTracking)(nil)).
				Set("last_checked = ?", time.Now()).
				Where("group_id IN (?)", bun.In(groupIDs)).
				Exec(ctx)
			if err != nil {
				r.logger.Error("Failed to update last_checked timestamps", zap.Error(err))
				return err
			}
		}

		// Map group IDs to their flagged user lists
		for _, tracking := range trackings {
			result[tracking.GroupID] = tracking.FlaggedUsers
		}

		return nil
	})
	if err != nil {
		r.logger.Error("Failed to get group trackings to check", zap.Error(err))
		return nil, err
	}

	return result, nil
}

// GetFlaggedUsers retrieves the list of flagged users for a specific group.
func (r *TrackingModel) GetFlaggedUsers(ctx context.Context, groupID uint64) ([]uint64, error) {
	var tracking types.GroupMemberTracking
	err := r.db.NewSelect().Model(&tracking).
		Column("flagged_users").
		Where("group_id = ?", groupID).
		Scan(ctx)
	if err != nil {
		r.logger.Error("Failed to get flagged users for group",
			zap.Error(err),
			zap.Uint64("groupID", groupID))
		return nil, err
	}

	return tracking.FlaggedUsers, nil
}

// UpdateFlaggedGroups marks the specified groups as flagged in the tracking table.
func (r *TrackingModel) UpdateFlaggedGroups(ctx context.Context, groupIDs []uint64) error {
	_, err := r.db.NewUpdate().Model((*types.GroupMemberTracking)(nil)).
		Set("is_flagged = true").
		Where("group_id IN (?)", bun.In(groupIDs)).
		Exec(ctx)
	if err != nil {
		r.logger.Error("Failed to update flagged groups", zap.Error(err))
		return err
	}
	return nil
}

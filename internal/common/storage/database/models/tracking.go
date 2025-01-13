package models

import (
	"context"
	"fmt"
	"sort"
	"time"

	apiTypes "github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
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
			ID:           groupID,
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
			Where("id IN (?)", bun.In(groupIDs)).
			For("UPDATE").
			Order("id").
			Scan(ctx)
		if err != nil {
			return err
		}

		// Perform bulk insert with upsert
		_, err = tx.NewInsert().
			Model(&trackings).
			On("CONFLICT (id) DO UPDATE").
			Set("flagged_users = ARRAY(SELECT DISTINCT unnest(EXCLUDED.flagged_users || group_member_tracking.flagged_users))").
			Set("last_appended = EXCLUDED.last_appended").
			Set("is_flagged = group_member_tracking.is_flagged").
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to add users to groups tracking: %w (groupCount=%d)", err, len(groupToUsers))
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
		return 0, fmt.Errorf(
			"failed to purge old group trackings: %w (cutoffDate=%s)",
			err, cutoffDate.Format(time.RFC3339),
		)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf(
			"failed to get rows affected: %w (cutoffDate=%s)",
			err, cutoffDate.Format(time.RFC3339),
		)
	}

	return int(rowsAffected), nil
}

// GetGroupTrackingsToCheck finds groups that haven't been checked recently
// with priority for groups with more flagged users.
func (r *TrackingModel) GetGroupTrackingsToCheck(ctx context.Context, batchSize int, minFlaggedUsers int, minFlaggedOverride int) (map[uint64][]uint64, error) {
	result := make(map[uint64][]uint64)

	now := time.Now()
	tenMinutesAgo := now.Add(-10 * time.Minute)
	oneMinuteAgo := now.Add(-1 * time.Minute)

	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var trackings []types.GroupMemberTracking

		// Build subquery to find the group IDs to update
		subq := tx.NewSelect().
			Model((*types.GroupMemberTracking)(nil)).
			Column("id").
			Where("is_flagged = FALSE").
			Where("cardinality(flagged_users) >= ?", minFlaggedUsers).
			Where("(last_checked < ? AND cardinality(flagged_users) >= ?) OR (last_checked < ? AND cardinality(flagged_users) >= ? / 2)",
				tenMinutesAgo, minFlaggedOverride,
				oneMinuteAgo, minFlaggedOverride).
			OrderExpr("cardinality(flagged_users) DESC").
			Order("last_checked ASC").
			Limit(batchSize)

		// Update the selected groups and return their data
		err := tx.NewUpdate().
			Model(&trackings).
			Set("last_checked = ?", now).
			Where("id IN (?)", subq).
			Returning("id, flagged_users").
			Scan(ctx)
		if err != nil {
			return fmt.Errorf("failed to get and update group trackings: %w", err)
		}

		// Map group IDs to their flagged user lists
		for _, tracking := range trackings {
			result[tracking.ID] = tracking.FlaggedUsers
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get group trackings to check: %w", err)
	}

	return result, nil
}

// GetFlaggedUsers retrieves the list of flagged users for a specific group.
func (r *TrackingModel) GetFlaggedUsers(ctx context.Context, groupID uint64) ([]uint64, error) {
	var tracking types.GroupMemberTracking
	err := r.db.NewSelect().Model(&tracking).
		Column("flagged_users").
		Where("id = ?", groupID).
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get flagged users for group: %w (groupID=%d)", err, groupID)
	}

	return tracking.FlaggedUsers, nil
}

// UpdateFlaggedGroups marks the specified groups as flagged in the tracking table.
func (r *TrackingModel) UpdateFlaggedGroups(ctx context.Context, groupIDs []uint64) error {
	_, err := r.db.NewUpdate().Model((*types.GroupMemberTracking)(nil)).
		Set("is_flagged = true").
		Where("id IN (?)", bun.In(groupIDs)).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to update flagged groups: %w (groupCount=%d)", err, len(groupIDs))
	}
	return nil
}

// RemoveUserFromGroups removes a user from the tracking lists of specified groups.
func (r *TrackingModel) RemoveUserFromGroups(ctx context.Context, userID uint64, groups []*apiTypes.UserGroupRoles) {
	if len(groups) == 0 {
		return
	}

	// Get all group IDs the user is in
	groupIDs := make([]uint64, 0, len(groups))
	for _, group := range groups {
		groupIDs = append(groupIDs, group.Group.ID)
	}

	// Remove user from group tracking
	_, err := r.db.NewUpdate().
		Model((*types.GroupMemberTracking)(nil)).
		Set("flagged_users = array_remove(flagged_users, ?)", userID).
		Where("id IN (?)", bun.In(groupIDs)).
		Exec(ctx)
	if err != nil {
		r.logger.Error("Failed to remove user from group tracking",
			zap.Error(err),
			zap.Uint64("userID", userID),
			zap.Uint64s("groupIDs", groupIDs))
	}

	r.logger.Debug("Removed user from group tracking",
		zap.Uint64("userID", userID),
		zap.Int("groupCount", len(groupIDs)))
}

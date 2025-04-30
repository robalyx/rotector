package models

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/robalyx/rotector/internal/database/types"
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
		logger: logger.Named("db_tracking"),
	}
}

// AddUsersToGroupsTracking adds multiple users to multiple groups' tracking lists.
func (r *TrackingModel) AddUsersToGroupsTracking(ctx context.Context, groupToUsers map[uint64][]uint64) error {
	// Create tracking entries for bulk insert
	trackings := make([]types.GroupMemberTracking, 0, len(groupToUsers))
	trackingUsers := make([]types.GroupMemberTrackingUser, 0)
	now := time.Now()

	for groupID, userIDs := range groupToUsers {
		trackings = append(trackings, types.GroupMemberTracking{
			ID:           groupID,
			LastAppended: now,
			IsFlagged:    false,
		})

		for _, userID := range userIDs {
			trackingUsers = append(trackingUsers, types.GroupMemberTrackingUser{
				GroupID: groupID,
				UserID:  userID,
			})
		}
	}

	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Lock the groups in a consistent order to prevent deadlocks
		groupIDs := make([]uint64, 0, len(groupToUsers))
		for groupID := range groupToUsers {
			groupIDs = append(groupIDs, groupID)
		}
		slices.Sort(groupIDs)

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
			Set("last_appended = EXCLUDED.last_appended").
			Set("is_flagged = group_member_tracking.is_flagged").
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to add tracking entries: %w", err)
		}

		_, err = tx.NewInsert().
			Model(&trackingUsers).
			On("CONFLICT DO NOTHING").
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to add tracking user entries: %w", err)
		}

		r.logger.Debug("Successfully processed group tracking updates",
			zap.Int("groupCount", len(groupToUsers)))

		return nil
	})
}

// GetGroupTrackingsToCheck finds groups that haven't been checked recently
// with priority for groups with more flagged users.
func (r *TrackingModel) GetGroupTrackingsToCheck(
	ctx context.Context, batchSize int, minFlaggedUsers int, minFlaggedOverride int,
) (map[uint64][]uint64, error) {
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
			With("user_counts", tx.NewSelect().
				Model((*types.GroupMemberTrackingUser)(nil)).
				Column("group_id").
				ColumnExpr("COUNT(*) as user_count").
				Group("group_id")).
			Join("JOIN user_counts ON group_member_tracking.id = user_counts.group_id").
			Where("is_flagged = FALSE").
			Where("user_count >= ?", minFlaggedUsers).
			Where("(last_checked < ? AND user_count >= ?) OR "+
				"(last_checked < ? AND user_count >= ? / 2)",
				tenMinutesAgo, minFlaggedOverride,
				oneMinuteAgo, minFlaggedOverride).
			OrderExpr("user_count DESC").
			Order("last_checked ASC").
			Limit(batchSize)

		// Update the selected groups and return their data
		err := tx.NewUpdate().
			Model(&trackings).
			Set("last_checked = ?", now).
			Where("id IN (?)", subq).
			Returning("id").
			Scan(ctx)
		if err != nil {
			return fmt.Errorf("failed to get and update group trackings: %w", err)
		}

		// Get flagged users for each group
		if len(trackings) > 0 {
			groupIDs := make([]uint64, len(trackings))
			for i, tracking := range trackings {
				groupIDs[i] = tracking.ID
			}

			var trackingUsers []types.GroupMemberTrackingUser
			err = tx.NewSelect().
				Model(&trackingUsers).
				Where("group_id IN (?)", bun.In(groupIDs)).
				Scan(ctx)
			if err != nil {
				return fmt.Errorf("failed to get tracking users: %w", err)
			}

			// Map users to their groups
			for _, tu := range trackingUsers {
				result[tu.GroupID] = append(result[tu.GroupID], tu.UserID)
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}

// GetFlaggedUsers retrieves the list of flagged users for a specific group.
func (r *TrackingModel) GetFlaggedUsers(ctx context.Context, groupID uint64) ([]uint64, error) {
	var trackingUsers []types.GroupMemberTrackingUser
	err := r.db.NewSelect().
		Model(&trackingUsers).
		Column("user_id").
		Where("group_id = ?", groupID).
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get flagged users for group: %w (groupID=%d)", err, groupID)
	}

	userIDs := make([]uint64, len(trackingUsers))
	for i, tu := range trackingUsers {
		userIDs[i] = tu.UserID
	}
	return userIDs, nil
}

// GetFlaggedUsersCount retrieves the count of flagged users for a specific group.
func (r *TrackingModel) GetFlaggedUsersCount(ctx context.Context, groupID uint64) (int, error) {
	count, err := r.db.NewSelect().
		Model((*types.GroupMemberTrackingUser)(nil)).
		Where("group_id = ?", groupID).
		Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get flagged users count for group: %w (groupID=%d)", err, groupID)
	}
	return count, nil
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

// RemoveUserFromAllGroups removes a user from all group tracking records.
func (r *TrackingModel) RemoveUserFromAllGroups(ctx context.Context, userID uint64) error {
	_, err := r.db.NewDelete().
		Model((*types.GroupMemberTrackingUser)(nil)).
		Where("user_id = ?", userID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to remove user from all group tracking: %w (userID=%d)", err, userID)
	}

	r.logger.Debug("Removed user from all group tracking",
		zap.Uint64("userID", userID))
	return nil
}

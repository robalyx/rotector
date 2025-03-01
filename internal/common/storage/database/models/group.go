package models

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
	"github.com/uptrace/bun"
	"go.uber.org/zap"
)

// GroupModel handles database operations for group records.
type GroupModel struct {
	db         *bun.DB
	activity   *ActivityModel
	reputation *ReputationModel
	votes      *VoteModel
	logger     *zap.Logger
}

// NewGroup creates a GroupModel with database access for
// storing and retrieving group information.
func NewGroup(db *bun.DB, activity *ActivityModel, reputation *ReputationModel, votes *VoteModel, logger *zap.Logger) *GroupModel {
	return &GroupModel{
		db:         db,
		activity:   activity,
		reputation: reputation,
		votes:      votes,
		logger:     logger,
	}
}

// SaveGroups updates or inserts groups into their appropriate tables based on their current status.
func (r *GroupModel) SaveGroups(ctx context.Context, groups map[uint64]*types.Group) error {
	// Get list of group IDs to check
	groupIDs := make([]uint64, 0, len(groups))
	for id := range groups {
		groupIDs = append(groupIDs, id)
	}

	// Get existing groups with all their data
	existingGroups, err := r.GetGroupsByIDs(ctx, groupIDs, types.GroupFieldBasic|types.GroupFieldTimestamps)
	if err != nil {
		return fmt.Errorf("failed to get existing groups: %w", err)
	}

	// Initialize slices for each table
	flaggedGroups := make([]*types.FlaggedGroup, 0)
	confirmedGroups := make([]*types.ConfirmedGroup, 0)
	clearedGroups := make([]*types.ClearedGroup, 0)
	counts := make(map[enum.GroupType]int)

	// Group groups by their target tables
	for id, group := range groups {
		// Generate UUID for new groups
		if group.UUID == uuid.Nil {
			group.UUID = uuid.New()
		}

		// Get existing group data if available
		var status enum.GroupType
		existingGroup := existingGroups[id]
		if existingGroup.Status != enum.GroupTypeUnflagged {
			status = existingGroup.Status
		} else {
			// Default to flagged_groups for new groups
			status = enum.GroupTypeFlagged
		}

		switch status {
		case enum.GroupTypeConfirmed:
			confirmedGroups = append(confirmedGroups, &types.ConfirmedGroup{
				Group:      *group,
				VerifiedAt: existingGroup.VerifiedAt,
			})
		case enum.GroupTypeFlagged:
			flaggedGroups = append(flaggedGroups, &types.FlaggedGroup{
				Group: *group,
			})
		case enum.GroupTypeCleared:
			clearedGroups = append(clearedGroups, &types.ClearedGroup{
				Group:     *group,
				ClearedAt: existingGroup.ClearedAt,
			})
		case enum.GroupTypeUnflagged:
			continue
		}
		counts[status]++
	}

	// Update each table
	err = r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Helper function to update a table
		updateTable := func(groups any, status enum.GroupType) error {
			if counts[status] == 0 {
				return nil
			}

			_, err := tx.NewInsert().
				Model(groups).
				On("CONFLICT (id) DO UPDATE").
				Set("uuid = EXCLUDED.uuid").
				Set("name = EXCLUDED.name").
				Set("description = EXCLUDED.description").
				Set("owner = EXCLUDED.owner").
				Set("shout = EXCLUDED.shout").
				Set("reasons = EXCLUDED.reasons").
				Set("confidence = EXCLUDED.confidence").
				Set("last_scanned = EXCLUDED.last_scanned").
				Set("last_updated = EXCLUDED.last_updated").
				Set("last_viewed = EXCLUDED.last_viewed").
				Set("last_lock_check = EXCLUDED.last_lock_check").
				Set("is_locked = EXCLUDED.is_locked").
				Set("thumbnail_url = EXCLUDED.thumbnail_url").
				Set("last_thumbnail_update = EXCLUDED.last_thumbnail_update").
				Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to update %s groups: %w", status, err)
			}
			return nil
		}

		// Update each table with its corresponding slice
		if err := updateTable(&flaggedGroups, enum.GroupTypeFlagged); err != nil {
			return err
		}
		if err := updateTable(&confirmedGroups, enum.GroupTypeConfirmed); err != nil {
			return err
		}
		if err := updateTable(&clearedGroups, enum.GroupTypeCleared); err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to save groups: %w", err)
	}

	r.logger.Debug("Successfully saved groups",
		zap.Int("totalGroups", len(groups)),
		zap.Int("flaggedGroups", counts[enum.GroupTypeFlagged]),
		zap.Int("confirmedGroups", counts[enum.GroupTypeConfirmed]),
		zap.Int("clearedGroups", counts[enum.GroupTypeCleared]))

	return nil
}

// ConfirmGroup moves a group from other group tables to confirmed_groups.
func (r *GroupModel) ConfirmGroup(ctx context.Context, group *types.ReviewGroup) error {
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		confirmedGroup := &types.ConfirmedGroup{
			Group:      group.Group,
			VerifiedAt: time.Now(),
		}

		// Try to move group to confirmed_groups table
		result, err := tx.NewInsert().Model(confirmedGroup).
			On("CONFLICT (id) DO NOTHING").
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to insert group in confirmed_groups: %w", err)
		}

		affected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("failed to get rows affected: %w", err)
		}
		if affected == 0 {
			return nil // Skip if there was a conflict
		}

		// Delete from other tables
		_, err = tx.NewDelete().Model((*types.FlaggedGroup)(nil)).Where("id = ?", group.ID).Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to delete group from flagged_groups: %w", err)
		}

		_, err = tx.NewDelete().Model((*types.ClearedGroup)(nil)).Where("id = ?", group.ID).Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to delete group from cleared_groups: %w", err)
		}

		return nil
	})
	if err != nil {
		return err
	}

	// Verify votes for the group
	if err := r.votes.VerifyVotes(ctx, group.ID, true, enum.VoteTypeGroup); err != nil {
		r.logger.Error("Failed to verify votes", zap.Error(err))
		return err
	}

	return nil
}

// ClearGroup moves a group from other group tables to cleared_groups.
func (r *GroupModel) ClearGroup(ctx context.Context, group *types.ReviewGroup) error {
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		clearedGroup := &types.ClearedGroup{
			Group:     group.Group,
			ClearedAt: time.Now(),
		}

		// Try to move group to cleared_groups table
		result, err := tx.NewInsert().Model(clearedGroup).
			On("CONFLICT (id) DO NOTHING").
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to insert group in cleared_groups: %w", err)
		}

		affected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("failed to get rows affected: %w", err)
		}
		if affected == 0 {
			return nil // Skip if there was a conflict
		}

		// Delete from other tables
		_, err = tx.NewDelete().Model((*types.FlaggedGroup)(nil)).Where("id = ?", group.ID).Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to delete group from flagged_groups: %w", err)
		}

		_, err = tx.NewDelete().Model((*types.ConfirmedGroup)(nil)).Where("id = ?", group.ID).Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to delete group from confirmed_groups: %w", err)
		}

		return nil
	})
	if err != nil {
		return err
	}

	// Verify votes for the group
	if err := r.votes.VerifyVotes(ctx, group.ID, false, enum.VoteTypeGroup); err != nil {
		r.logger.Error("Failed to verify votes", zap.Error(err))
		return err
	}

	return nil
}

// GetGroupByID retrieves a group by either their numeric ID or UUID.
func (r *GroupModel) GetGroupByID(ctx context.Context, groupID string, fields types.GroupField) (*types.ReviewGroup, error) {
	var result types.ReviewGroup

	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Try each model in order until we find a group
		models := []any{
			&types.FlaggedGroup{},
			&types.ConfirmedGroup{},
			&types.ClearedGroup{},
		}

		for _, model := range models {
			query := tx.NewSelect().
				Model(model).
				Column(fields.Columns()...).
				For("UPDATE")

			// Check if input is numeric (ID) or string (UUID)
			if id, err := strconv.ParseUint(groupID, 10, 64); err == nil {
				query.Where("id = ?", id)
			} else {
				// Parse UUID string
				uid, err := uuid.Parse(groupID)
				if err != nil {
					return types.ErrInvalidGroupID
				}
				query.Where("uuid = ?", uid)
			}

			err := query.Scan(ctx)
			if err == nil {
				// Set result based on model type
				switch m := model.(type) {
				case *types.FlaggedGroup:
					result.Group = m.Group
					result.Status = enum.GroupTypeFlagged
				case *types.ConfirmedGroup:
					result.Group = m.Group
					result.VerifiedAt = m.VerifiedAt
					result.Status = enum.GroupTypeConfirmed
				case *types.ClearedGroup:
					result.Group = m.Group
					result.ClearedAt = m.ClearedAt
					result.Status = enum.GroupTypeCleared
				}

				// Get reputation if requested
				if fields.HasReputation() {
					reputation, err := r.reputation.GetGroupReputation(ctx, result.ID)
					if err != nil {
						return fmt.Errorf("failed to get group reputation: %w", err)
					}
					result.Reputation = reputation
				}

				// Update last_viewed if requested
				_, err = tx.NewUpdate().
					Model(model).
					Set("last_viewed = ?", time.Now()).
					Where("id = ?", result.ID).
					Exec(ctx)
				if err != nil {
					return err
				}

				return nil
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}

		return types.ErrGroupNotFound
	})
	if err != nil {
		return nil, err
	}

	return &result, nil
}

// GetGroupsByIDs retrieves specified group information for a list of group IDs.
// Returns a map of group IDs to review groups.
func (r *GroupModel) GetGroupsByIDs(ctx context.Context, groupIDs []uint64, fields types.GroupField) (map[uint64]*types.ReviewGroup, error) {
	groups := make(map[uint64]*types.ReviewGroup)

	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Build query with selected fields
		columns := fields.Columns()

		// Query confirmed groups
		var confirmedGroups []types.ConfirmedGroup
		err := tx.NewSelect().
			Model(&confirmedGroups).
			Column(columns...).
			Where("id IN (?)", bun.In(groupIDs)).
			Scan(ctx)
		if err != nil {
			return fmt.Errorf("failed to get confirmed groups: %w", err)
		}
		for _, group := range confirmedGroups {
			groups[group.ID] = &types.ReviewGroup{
				Group:      group.Group,
				VerifiedAt: group.VerifiedAt,
				Status:     enum.GroupTypeConfirmed,
			}
		}

		// Query flagged groups
		var flaggedGroups []types.FlaggedGroup
		err = tx.NewSelect().
			Model(&flaggedGroups).
			Column(columns...).
			Where("id IN (?)", bun.In(groupIDs)).
			Scan(ctx)
		if err != nil {
			return fmt.Errorf("failed to get flagged groups: %w", err)
		}
		for _, group := range flaggedGroups {
			groups[group.ID] = &types.ReviewGroup{
				Group:  group.Group,
				Status: enum.GroupTypeFlagged,
			}
		}

		// Query cleared groups
		var clearedGroups []types.ClearedGroup
		err = tx.NewSelect().
			Model(&clearedGroups).
			Column(columns...).
			Where("id IN (?)", bun.In(groupIDs)).
			Scan(ctx)
		if err != nil {
			return fmt.Errorf("failed to get cleared groups: %w", err)
		}
		for _, group := range clearedGroups {
			groups[group.ID] = &types.ReviewGroup{
				Group:     group.Group,
				ClearedAt: group.ClearedAt,
				Status:    enum.GroupTypeCleared,
			}
		}

		// Mark remaining IDs as unflagged
		for _, id := range groupIDs {
			if _, ok := groups[id]; !ok {
				groups[id] = &types.ReviewGroup{
					Group:  types.Group{ID: id},
					Status: enum.GroupTypeUnflagged,
				}
			}
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get groups by IDs: %w (groupCount=%d)", err, len(groupIDs))
	}

	r.logger.Debug("Retrieved groups by IDs",
		zap.Int("requestedCount", len(groupIDs)),
		zap.Int("foundCount", len(groups)))

	return groups, nil
}

// GetFlaggedAndConfirmedGroups retrieves all flagged and confirmed groups.
func (r *GroupModel) GetFlaggedAndConfirmedGroups(ctx context.Context) ([]*types.ReviewGroup, error) {
	var groups []*types.ReviewGroup

	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Get flagged groups
		var flaggedGroups []types.FlaggedGroup
		err := tx.NewSelect().
			Model(&flaggedGroups).
			Column("id", "reasons", "confidence").
			Scan(ctx)
		if err != nil {
			return fmt.Errorf("failed to get flagged groups: %w", err)
		}
		for _, group := range flaggedGroups {
			groups = append(groups, &types.ReviewGroup{
				Group:  group.Group,
				Status: enum.GroupTypeFlagged,
			})
		}

		// Get confirmed groups
		var confirmedGroups []types.ConfirmedGroup
		err = tx.NewSelect().
			Model(&confirmedGroups).
			Column("id", "reasons", "confidence").
			Scan(ctx)
		if err != nil {
			return fmt.Errorf("failed to get confirmed groups: %w", err)
		}
		for _, group := range confirmedGroups {
			groups = append(groups, &types.ReviewGroup{
				Group:  group.Group,
				Status: enum.GroupTypeConfirmed,
			})
		}

		return nil
	})

	return groups, err
}

// GetGroupsToCheck finds groups that haven't been checked for locked status recently.
// Returns two slices: groups to check, and currently locked groups among those to check.
func (r *GroupModel) GetGroupsToCheck(ctx context.Context, limit int) ([]uint64, []uint64, error) {
	var groupIDs []uint64
	var lockedIDs []uint64
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Get and update confirmed groups
		var confirmedGroups []types.ConfirmedGroup
		err := tx.NewSelect().
			Model(&confirmedGroups).
			Column("id", "is_locked").
			Where("last_lock_check < NOW() - INTERVAL '1 day'").
			OrderExpr("last_lock_check ASC").
			Limit(limit / 2).
			For("UPDATE SKIP LOCKED").
			Scan(ctx)
		if err != nil {
			return fmt.Errorf("failed to get confirmed groups: %w", err)
		}

		if len(confirmedGroups) > 0 {
			groupIDs = make([]uint64, 0, len(confirmedGroups))
			for _, group := range confirmedGroups {
				groupIDs = append(groupIDs, group.ID)
				if group.IsLocked {
					lockedIDs = append(lockedIDs, group.ID)
				}
			}

			// Update last_lock_check
			_, err = tx.NewUpdate().
				Model(&confirmedGroups).
				Set("last_lock_check = NOW()").
				Where("id IN (?)", bun.In(groupIDs)).
				Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to update confirmed groups: %w", err)
			}
		}

		// Calculate remaining limit for flagged groups
		remainingLimit := limit - len(confirmedGroups)
		if remainingLimit <= 0 {
			return nil
		}

		// Get and update flagged groups
		var flaggedGroups []types.FlaggedGroup
		err = tx.NewSelect().
			Model(&flaggedGroups).
			Column("id", "is_locked").
			Where("last_lock_check < NOW() - INTERVAL '1 day'").
			OrderExpr("last_lock_check ASC").
			Limit(remainingLimit).
			For("UPDATE SKIP LOCKED").
			Scan(ctx)
		if err != nil {
			return fmt.Errorf("failed to get flagged groups: %w", err)
		}

		if len(flaggedGroups) > 0 {
			flaggedIDs := make([]uint64, 0, len(flaggedGroups))
			for _, group := range flaggedGroups {
				flaggedIDs = append(flaggedIDs, group.ID)
				if group.IsLocked {
					lockedIDs = append(lockedIDs, group.ID)
				}
			}

			// Update last_lock_check
			_, err = tx.NewUpdate().
				Model(&flaggedGroups).
				Set("last_lock_check = NOW()").
				Where("id IN (?)", bun.In(flaggedIDs)).
				Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to update flagged groups: %w", err)
			}

			groupIDs = append(groupIDs, flaggedIDs...)
		}

		return nil
	})

	return groupIDs, lockedIDs, err
}

// MarkGroupsLockStatus updates the locked status of groups in their respective tables.
func (r *GroupModel) MarkGroupsLockStatus(ctx context.Context, groupIDs []uint64, isLocked bool) error {
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Update confirmed groups
		_, err := tx.NewUpdate().
			Model((*types.ConfirmedGroup)(nil)).
			Set("is_locked = ?", isLocked).
			Where("id IN (?)", bun.In(groupIDs)).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to mark confirmed groups lock status: %w", err)
		}

		// Update flagged groups
		_, err = tx.NewUpdate().
			Model((*types.FlaggedGroup)(nil)).
			Set("is_locked = ?", isLocked).
			Where("id IN (?)", bun.In(groupIDs)).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to mark flagged groups lock status: %w", err)
		}

		r.logger.Debug("Marked groups lock status",
			zap.Int("count", len(groupIDs)),
			zap.Bool("isLocked", isLocked))
		return nil
	})
}

// GetLockedCount returns the total number of locked groups across all tables
func (r *GroupModel) GetLockedCount(ctx context.Context) (int, error) {
	count, err := r.db.NewSelect().
		TableExpr("(?) AS locked_groups", r.db.NewSelect().
			Model((*types.ConfirmedGroup)(nil)).
			Column("id").
			Where("is_locked = true").
			UnionAll(
				r.db.NewSelect().
					Model((*types.FlaggedGroup)(nil)).
					Column("id").
					Where("is_locked = true"),
			),
		).
		Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get locked groups count: %w", err)
	}

	return count, nil
}

// GetGroupCounts returns counts for all group statuses
func (r *GroupModel) GetGroupCounts(ctx context.Context) (*types.GroupCounts, error) {
	var counts types.GroupCounts

	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		confirmedCount, err := tx.NewSelect().Model((*types.ConfirmedGroup)(nil)).Count(ctx)
		if err != nil {
			return fmt.Errorf("failed to get confirmed groups count: %w", err)
		}
		counts.Confirmed = confirmedCount

		flaggedCount, err := tx.NewSelect().Model((*types.FlaggedGroup)(nil)).Count(ctx)
		if err != nil {
			return fmt.Errorf("failed to get flagged groups count: %w", err)
		}
		counts.Flagged = flaggedCount

		clearedCount, err := tx.NewSelect().Model((*types.ClearedGroup)(nil)).Count(ctx)
		if err != nil {
			return fmt.Errorf("failed to get cleared groups count: %w", err)
		}
		counts.Cleared = clearedCount

		lockedCount, err := r.GetLockedCount(ctx)
		if err != nil {
			return err
		}
		counts.Locked = lockedCount

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get group counts: %w", err)
	}

	return &counts, nil
}

// PurgeOldClearedGroups removes cleared groups older than the cutoff date.
// This helps maintain database size by removing groups that were cleared long ago.
func (r *GroupModel) PurgeOldClearedGroups(ctx context.Context, cutoffDate time.Time) (int, error) {
	result, err := r.db.NewDelete().
		Model((*types.ClearedGroup)(nil)).
		Where("cleared_at < ?", cutoffDate).
		Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf(
			"failed to purge old cleared groups: %w (cutoffDate=%s)",
			err, cutoffDate.Format(time.RFC3339),
		)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}

	r.logger.Debug("Purged old cleared groups",
		zap.Int64("rowsAffected", affected),
		zap.Time("cutoffDate", cutoffDate))

	return int(affected), nil
}

// GetGroupsForThumbnailUpdate retrieves groups that need thumbnail updates.
func (r *GroupModel) GetGroupsForThumbnailUpdate(ctx context.Context, limit int) (map[uint64]*types.Group, error) {
	groups := make(map[uint64]*types.Group)

	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Query groups from each table that need thumbnail updates
		for _, model := range []any{
			(*types.FlaggedGroup)(nil),
			(*types.ConfirmedGroup)(nil),
			(*types.ClearedGroup)(nil),
		} {
			var reviewGroups []types.ReviewGroup
			err := tx.NewSelect().
				Model(model).
				Where("last_thumbnail_update < NOW() - INTERVAL '7 days'").
				OrderExpr("last_thumbnail_update ASC").
				Limit(limit).
				Scan(ctx, &reviewGroups)

			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("failed to query groups for thumbnail update: %w", err)
			}

			for _, group := range reviewGroups {
				groups[group.ID] = &group.Group
			}
		}
		return nil
	})

	return groups, err
}

// DeleteGroup removes a group and all associated data from the database.
func (r *GroupModel) DeleteGroup(ctx context.Context, groupID uint64) (bool, error) {
	var totalAffected int64
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Delete from flagged_groups
		result, err := tx.NewDelete().
			Model((*types.FlaggedGroup)(nil)).
			Where("id = ?", groupID).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to delete from flagged_groups: %w", err)
		}
		affected, _ := result.RowsAffected()
		totalAffected += affected

		// Delete from confirmed_groups
		result, err = tx.NewDelete().
			Model((*types.ConfirmedGroup)(nil)).
			Where("id = ?", groupID).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to delete from confirmed_groups: %w", err)
		}
		affected, _ = result.RowsAffected()
		totalAffected += affected

		// Delete from cleared_groups
		result, err = tx.NewDelete().
			Model((*types.ClearedGroup)(nil)).
			Where("id = ?", groupID).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to delete from cleared_groups: %w", err)
		}
		affected, _ = result.RowsAffected()
		totalAffected += affected

		return nil
	})

	return totalAffected > 0, err
}

// GetGroupToScan finds the next group to scan from confirmed_groups, falling back to flagged_groups
// if no confirmed groups are available.
func (r *GroupModel) GetGroupToScan(ctx context.Context) (*types.Group, error) {
	var group *types.Group
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// First try confirmed groups
		var confirmedGroup types.ConfirmedGroup
		err := tx.NewSelect().Model(&confirmedGroup).
			Where("last_scanned < NOW() - INTERVAL '1 day'").
			OrderExpr("last_scanned ASC, confidence DESC").
			Limit(1).
			For("UPDATE SKIP LOCKED").
			Scan(ctx)
		if err == nil {
			// Update last_scanned
			_, err = tx.NewUpdate().Model(&confirmedGroup).
				Set("last_scanned = ?", time.Now()).
				Where("id = ?", confirmedGroup.ID).
				Exec(ctx)
			if err != nil {
				return fmt.Errorf(
					"failed to update last_scanned for confirmed group: %w (groupID=%d)",
					err, confirmedGroup.ID,
				)
			}
			group = &confirmedGroup.Group
			return nil
		}

		// If no confirmed groups, try flagged groups
		var flaggedGroup types.FlaggedGroup
		err = tx.NewSelect().Model(&flaggedGroup).
			Where("last_scanned < NOW() - INTERVAL '1 day'").
			OrderExpr("last_scanned ASC, confidence DESC").
			Limit(1).
			For("UPDATE SKIP LOCKED").
			Scan(ctx)
		if err != nil {
			return fmt.Errorf("failed to get group to scan: %w", err)
		}

		// Update last_scanned
		_, err = tx.NewUpdate().Model(&flaggedGroup).
			Set("last_scanned = ?", time.Now()).
			Where("id = ?", flaggedGroup.ID).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf(
				"failed to update last_scanned for flagged group: %w (groupID=%d)",
				err, flaggedGroup.ID,
			)
		}
		group = &flaggedGroup.Group
		return nil
	})
	if err != nil {
		return nil, err
	}

	return group, nil
}

// CheckConfirmedGroups checks which groups from a list of IDs exist in any group table.
// Returns a map of group IDs to their status (confirmed, flagged, cleared, locked).
func (r *GroupModel) CheckConfirmedGroups(ctx context.Context, groupIDs []uint64) ([]uint64, error) {
	var confirmedGroupIDs []uint64

	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Query confirmed groups
		err := tx.NewSelect().
			Model((*types.ConfirmedGroup)(nil)).
			Column("id").
			Where("id IN (?)", bun.In(groupIDs)).
			Scan(ctx, &confirmedGroupIDs)
		if err != nil {
			return fmt.Errorf("failed to query confirmed groups: %w", err)
		}

		return nil
	})

	return confirmedGroupIDs, err
}

// GetGroupToReview finds a group to review based on the sort method and target mode.
func (r *GroupModel) GetGroupToReview(ctx context.Context, sortBy enum.ReviewSortBy, targetMode enum.ReviewTargetMode, reviewerID uint64) (*types.ReviewGroup, error) {
	// Get recently reviewed group IDs
	recentIDs, err := r.activity.GetRecentlyReviewedIDs(ctx, reviewerID, true, 100)
	if err != nil {
		r.logger.Error("Failed to get recently reviewed group IDs", zap.Error(err))
		// Continue without filtering if there's an error
		recentIDs = []uint64{}
	}

	// Define models in priority order based on target mode
	var models []any
	switch targetMode {
	case enum.ReviewTargetModeFlagged:
		models = []any{
			&types.FlaggedGroup{},   // Primary target
			&types.ConfirmedGroup{}, // First fallback
			&types.ClearedGroup{},   // Second fallback
		}
	case enum.ReviewTargetModeConfirmed:
		models = []any{
			&types.ConfirmedGroup{}, // Primary target
			&types.FlaggedGroup{},   // First fallback
			&types.ClearedGroup{},   // Second fallback
		}
	case enum.ReviewTargetModeCleared:
		models = []any{
			&types.ClearedGroup{},   // Primary target
			&types.FlaggedGroup{},   // First fallback
			&types.ConfirmedGroup{}, // Second fallback
		}
	}

	// Try each model in order until we find a group
	for _, model := range models {
		result, err := r.getNextToReview(ctx, model, sortBy, recentIDs)
		if err == nil {
			return result, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}

	return nil, types.ErrNoGroupsToReview
}

// getNextToReview handles the common logic for getting the next item to review.
func (r *GroupModel) getNextToReview(ctx context.Context, model any, sortBy enum.ReviewSortBy, recentIDs []uint64) (*types.ReviewGroup, error) {
	var result types.ReviewGroup
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Build subquery to get ID
		subq := tx.NewSelect().
			Model(model).
			Column("id")

		// Exclude recently reviewed IDs if any exist
		if len(recentIDs) > 0 {
			subq.Where("?TableAlias.id NOT IN (?)", bun.In(recentIDs))
		}

		// Apply sort order to subquery
		switch sortBy {
		case enum.ReviewSortByConfidence:
			subq.Order("confidence DESC")
		case enum.ReviewSortByLastUpdated:
			subq.Order("last_updated ASC")
		case enum.ReviewSortByReputation:
			subq.Join("LEFT JOIN group_reputations ON group_reputations.id = ?TableAlias.id").
				OrderExpr("COALESCE(group_reputations.score, 0) ASC")
		case enum.ReviewSortByLastViewed:
			subq.Order("last_viewed ASC")
		case enum.ReviewSortByRandom:
			subq.OrderExpr("RANDOM()")
		}

		subq.Limit(1)

		// Main query to get the full record with FOR UPDATE
		err := tx.NewSelect().
			Model(model).
			Where("id = (?)", subq).
			For("UPDATE").
			Scan(ctx)
		if err != nil {
			return err
		}

		// Set result based on model type
		switch m := model.(type) {
		case *types.FlaggedGroup:
			result.Group = m.Group
			result.Status = enum.GroupTypeFlagged
		case *types.ConfirmedGroup:
			result.Group = m.Group
			result.VerifiedAt = m.VerifiedAt
			result.Status = enum.GroupTypeConfirmed
		case *types.ClearedGroup:
			result.Group = m.Group
			result.ClearedAt = m.ClearedAt
			result.Status = enum.GroupTypeCleared
		default:
			return fmt.Errorf("%w: %T", types.ErrUnsupportedModel, model)
		}

		// Get reputation
		reputation, err := r.reputation.GetGroupReputation(ctx, result.ID)
		if err != nil {
			return fmt.Errorf("failed to get group reputation: %w", err)
		}
		result.Reputation = reputation

		// Update last_viewed
		_, err = tx.NewUpdate().
			Model(model).
			Set("last_viewed = ?", time.Now()).
			Where("id = ?", result.ID).
			Exec(ctx)
		if err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return &result, nil
}

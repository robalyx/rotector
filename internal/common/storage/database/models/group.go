package models

import (
	"context"
	"fmt"
	"time"

	"github.com/rotector/rotector/internal/common/storage/database/types"
	"github.com/uptrace/bun"
	"go.uber.org/zap"
)

// GroupModel handles database operations for group records.
type GroupModel struct {
	db     *bun.DB
	logger *zap.Logger
}

// NewGroup creates a GroupModel with database access for
// storing and retrieving group information.
func NewGroup(db *bun.DB, logger *zap.Logger) *GroupModel {
	return &GroupModel{
		db:     db,
		logger: logger,
	}
}

// SaveFlaggedGroups adds or updates groups in the flagged_groups table.
// For each group, it updates all fields if the group already exists,
// or inserts a new record if they don't.
func (r *GroupModel) SaveFlaggedGroups(ctx context.Context, flaggedGroups []*types.FlaggedGroup) error {
	r.logger.Debug("Saving flagged groups", zap.Int("count", len(flaggedGroups)))

	// Perform bulk insert with upsert
	_, err := r.db.NewInsert().
		Model(&flaggedGroups).
		On("CONFLICT (id) DO UPDATE").
		Set("name = EXCLUDED.name").
		Set("description = EXCLUDED.description").
		Set("owner = EXCLUDED.owner").
		Set("shout = EXCLUDED.shout").
		Set("reason = EXCLUDED.reason").
		Set("confidence = EXCLUDED.confidence").
		Set("last_scanned = EXCLUDED.last_scanned").
		Set("last_updated = EXCLUDED.last_updated").
		Set("last_viewed = EXCLUDED.last_viewed").
		Set("last_purge_check = EXCLUDED.last_purge_check").
		Set("thumbnail_url = EXCLUDED.thumbnail_url").
		Set("upvotes = EXCLUDED.upvotes").
		Set("downvotes = EXCLUDED.downvotes").
		Set("reputation = EXCLUDED.reputation").
		Exec(ctx)
	if err != nil {
		r.logger.Error("Failed to save flagged groups",
			zap.Error(err),
			zap.Int("groupCount", len(flaggedGroups)))
		return err
	}

	r.logger.Debug("Finished saving flagged groups")
	return nil
}

// ConfirmGroup moves a group from flagged_groups to confirmed_groups.
// This happens when a moderator confirms that a group is inappropriate.
func (r *GroupModel) ConfirmGroup(ctx context.Context, group *types.FlaggedGroup) error {
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		confirmedGroup := &types.ConfirmedGroup{
			Group:      group.Group,
			VerifiedAt: time.Now(),
		}

		_, err := tx.NewInsert().Model(confirmedGroup).
			On("CONFLICT (id) DO UPDATE").
			Set("name = EXCLUDED.name").
			Set("description = EXCLUDED.description").
			Set("owner = EXCLUDED.owner").
			Set("reason = EXCLUDED.reason").
			Set("confidence = EXCLUDED.confidence").
			Set("last_scanned = EXCLUDED.last_scanned").
			Set("last_updated = EXCLUDED.last_updated").
			Set("last_viewed = EXCLUDED.last_viewed").
			Set("thumbnail_url = EXCLUDED.thumbnail_url").
			Set("upvotes = EXCLUDED.upvotes").
			Set("downvotes = EXCLUDED.downvotes").
			Set("reputation = EXCLUDED.reputation").
			Set("verified_at = EXCLUDED.verified_at").
			Exec(ctx)
		if err != nil {
			r.logger.Error("Failed to insert or update group in confirmed_groups",
				zap.Error(err),
				zap.Uint64("groupID", group.ID))
			return err
		}

		_, err = tx.NewDelete().Model((*types.FlaggedGroup)(nil)).
			Where("id = ?", group.ID).
			Exec(ctx)
		if err != nil {
			r.logger.Error("Failed to delete group from flagged_groups",
				zap.Error(err),
				zap.Uint64("groupID", group.ID))
			return err
		}

		return nil
	})
}

// ClearGroup moves a group from flagged_groups to cleared_groups.
// This happens when a moderator determines that a group was incorrectly flagged.
func (r *GroupModel) ClearGroup(ctx context.Context, group *types.FlaggedGroup) error {
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		clearedGroup := &types.ClearedGroup{
			Group:     group.Group,
			ClearedAt: time.Now(),
		}

		_, err := tx.NewInsert().Model(clearedGroup).
			On("CONFLICT (id) DO UPDATE").
			Set("name = EXCLUDED.name").
			Set("description = EXCLUDED.description").
			Set("owner = EXCLUDED.owner").
			Set("reason = EXCLUDED.reason").
			Set("confidence = EXCLUDED.confidence").
			Set("last_scanned = EXCLUDED.last_scanned").
			Set("last_updated = EXCLUDED.last_updated").
			Set("last_viewed = EXCLUDED.last_viewed").
			Set("thumbnail_url = EXCLUDED.thumbnail_url").
			Set("upvotes = EXCLUDED.upvotes").
			Set("downvotes = EXCLUDED.downvotes").
			Set("reputation = EXCLUDED.reputation").
			Set("cleared_at = EXCLUDED.cleared_at").
			Exec(ctx)
		if err != nil {
			r.logger.Error("Failed to insert or update group in cleared_groups", zap.Error(err), zap.Uint64("groupID", group.ID))
			return err
		}

		_, err = tx.NewDelete().Model((*types.FlaggedGroup)(nil)).Where("id = ?", group.ID).Exec(ctx)
		if err != nil {
			r.logger.Error("Failed to delete group from flagged_groups", zap.Error(err), zap.Uint64("groupID", group.ID))
			return err
		}

		r.logger.Debug("Group cleared and moved to cleared_groups", zap.Uint64("groupID", group.ID))

		return nil
	})
}

// GetClearedGroupByID finds a group in the cleared_groups table by their ID.
func (r *GroupModel) GetClearedGroupByID(ctx context.Context, id uint64) (*types.ClearedGroup, error) {
	var group types.ClearedGroup
	err := r.db.NewSelect().
		Model(&group).
		Where("id = ?", id).
		Scan(ctx)
	if err != nil {
		r.logger.Error("Failed to get cleared group by ID", zap.Error(err), zap.Uint64("groupID", id))
		return nil, err
	}
	r.logger.Debug("Retrieved cleared group by ID", zap.Uint64("groupID", id))
	return &group, nil
}

// GetClearedGroupsCount returns the total number of groups in cleared_groups.
func (r *GroupModel) GetClearedGroupsCount(ctx context.Context) (int, error) {
	count, err := r.db.NewSelect().
		Model((*types.ClearedGroup)(nil)).
		Count(ctx)
	if err != nil {
		r.logger.Error("Failed to get cleared groups count", zap.Error(err))
		return 0, err
	}
	return count, nil
}

// GetGroupByID retrieves a group from any group table by their ID.
// It checks all group tables (flagged, confirmed, cleared, locked) and returns the group with their status.
func (r *GroupModel) GetGroupByID(ctx context.Context, groupID uint64, fields types.GroupFields) (*types.ConfirmedGroup, error) {
	// Try each group table in order
	tables := []struct {
		model interface{}
		name  string
	}{
		{(*types.FlaggedGroup)(nil), "flagged_groups"},
		{(*types.ConfirmedGroup)(nil), "confirmed_groups"},
		{(*types.ClearedGroup)(nil), "cleared_groups"},
		{(*types.LockedGroup)(nil), "locked_groups"},
	}

	// Build query with selected fields
	columns := fields.Columns()
	if len(columns) == 0 {
		columns = []string{"*"} // Select all if no fields specified
	}

	// Try each table until we find the group
	for _, table := range tables {
		var group types.ConfirmedGroup
		err := r.db.NewSelect().
			Model(table.model).
			ModelTableExpr(table.name).
			Column(columns...).
			Where("id = ?", groupID).
			Scan(ctx, &group)

		if err == nil {
			// Update last viewed timestamp
			_, err = r.db.NewUpdate().
				Model(table.model).
				ModelTableExpr(table.name).
				Set("last_viewed = ?", time.Now()).
				Where("id = ?", groupID).
				Exec(ctx)
			if err != nil {
				r.logger.Error("Failed to update last_viewed timestamp",
					zap.Error(err),
					zap.Uint64("group_id", groupID))
			}

			return &group, nil
		}

		r.logger.Error("Error querying group table",
			zap.Error(err),
			zap.String("table", table.name),
			zap.Uint64("group_id", groupID))
	}

	return nil, types.ErrGroupNotFound
}

// GetGroupsByIDs retrieves specified group information for a list of group IDs.
// Returns a map of group IDs to group data and a separate map for their types.
func (r *GroupModel) GetGroupsByIDs(ctx context.Context, groupIDs []uint64, fields types.GroupFields) (map[uint64]*types.Group, map[uint64]types.GroupType, error) {
	groups := make(map[uint64]*types.Group)
	groupTypes := make(map[uint64]types.GroupType)

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
			groups[group.ID] = &group.Group
			groupTypes[group.ID] = types.GroupTypeConfirmed
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
			groups[group.ID] = &group.Group
			groupTypes[group.ID] = types.GroupTypeFlagged
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
			groups[group.ID] = &group.Group
			groupTypes[group.ID] = types.GroupTypeCleared
		}

		// Query locked groups
		var lockedGroups []types.LockedGroup
		err = tx.NewSelect().
			Model(&lockedGroups).
			Column(columns...).
			Where("id IN (?)", bun.In(groupIDs)).
			Scan(ctx)
		if err != nil {
			return fmt.Errorf("failed to get locked groups: %w", err)
		}
		for _, group := range lockedGroups {
			groups[group.ID] = &group.Group
			groupTypes[group.ID] = types.GroupTypeLocked
		}

		// Mark remaining IDs as unflagged
		for _, id := range groupIDs {
			if _, ok := groupTypes[id]; !ok {
				groupTypes[id] = types.GroupTypeUnflagged
			}
		}

		return nil
	})
	if err != nil {
		r.logger.Error("Failed to get groups by IDs",
			zap.Error(err),
			zap.Uint64s("groupIDs", groupIDs))
		return nil, nil, err
	}

	r.logger.Debug("Retrieved groups by IDs",
		zap.Int("requestedCount", len(groupIDs)),
		zap.Int("foundCount", len(groups)))

	return groups, groupTypes, nil
}

// UpdateTrainingVotes updates the upvotes or downvotes count for a group in training mode.
func (r *GroupModel) UpdateTrainingVotes(ctx context.Context, groupID uint64, isUpvote bool) error {
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Try to update votes in either flagged or confirmed table
		if err := r.updateVotesInTable(ctx, tx, (*types.FlaggedGroup)(nil), groupID, isUpvote); err == nil {
			return nil
		}
		return r.updateVotesInTable(ctx, tx, (*types.ConfirmedGroup)(nil), groupID, isUpvote)
	})
	if err != nil {
		r.logger.Error("Failed to update training votes",
			zap.Error(err),
			zap.Uint64("groupID", groupID),
			zap.String("voteType", map[bool]string{true: "upvote", false: "downvote"}[isUpvote]))
	}
	return err
}

// updateVotesInTable handles updating votes for a specific table type.
func (r *GroupModel) updateVotesInTable(ctx context.Context, tx bun.Tx, model interface{}, groupID uint64, isUpvote bool) error {
	// Get current vote counts
	var upvotes, downvotes int
	err := tx.NewSelect().
		Model(model).
		Column("upvotes", "downvotes").
		Where("id = ?", groupID).
		Scan(ctx, &upvotes, &downvotes)
	if err != nil {
		return err
	}

	// Update vote counts
	if isUpvote {
		upvotes++
	} else {
		downvotes++
	}
	reputation := upvotes - downvotes

	// Save updated counts
	_, err = tx.NewUpdate().
		Model(model).
		Set("upvotes = ?", upvotes).
		Set("downvotes = ?", downvotes).
		Set("reputation = ?", reputation).
		Where("id = ?", groupID).
		Exec(ctx)
	return err
}

// GetGroupsToCheck finds groups that haven't been checked for locked status recently.
func (r *GroupModel) GetGroupsToCheck(ctx context.Context, limit int) ([]uint64, error) {
	var groupIDs []uint64
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Get and update confirmed groups
		err := tx.NewRaw(`
			WITH updated AS (
				UPDATE confirmed_groups
				SET last_purge_check = NOW()
				WHERE id IN (
					SELECT id FROM confirmed_groups
					WHERE last_purge_check < NOW() - INTERVAL '1 day'
					ORDER BY last_purge_check ASC
					LIMIT ?
					FOR UPDATE SKIP LOCKED
				)
				RETURNING id
			)
			SELECT * FROM updated
		`, limit/2).Scan(ctx, &groupIDs)
		if err != nil {
			r.logger.Error("Failed to get and update confirmed groups", zap.Error(err))
			return err
		}

		// Get and update flagged groups
		var flaggedIDs []uint64
		err = tx.NewRaw(`
			WITH updated AS (
				UPDATE flagged_groups
				SET last_purge_check = NOW()
				WHERE id IN (
					SELECT id FROM flagged_groups
					WHERE last_purge_check < NOW() - INTERVAL '1 day'
					ORDER BY last_purge_check ASC
					LIMIT ?
					FOR UPDATE SKIP LOCKED
				)
				RETURNING id
			)
			SELECT * FROM updated
		`, limit/2).Scan(ctx, &flaggedIDs)
		if err != nil {
			r.logger.Error("Failed to get and update flagged groups", zap.Error(err))
			return err
		}
		groupIDs = append(groupIDs, flaggedIDs...)

		return nil
	})

	return groupIDs, err
}

// PurgeOldClearedGroups removes cleared groups older than the cutoff date.
// This helps maintain database size by removing groups that were cleared long ago.
func (r *GroupModel) PurgeOldClearedGroups(ctx context.Context, cutoffDate time.Time) (int, error) {
	result, err := r.db.NewDelete().
		Model((*types.ClearedGroup)(nil)).
		Where("cleared_at < ?", cutoffDate).
		Exec(ctx)
	if err != nil {
		r.logger.Error("Failed to purge old cleared groups",
			zap.Error(err),
			zap.Time("cutoffDate", cutoffDate))
		return 0, err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		r.logger.Error("Failed to get rows affected", zap.Error(err))
		return 0, err
	}

	r.logger.Debug("Purged old cleared groups",
		zap.Int64("rowsAffected", affected),
		zap.Time("cutoffDate", cutoffDate))

	return int(affected), nil
}

// RemoveLockedGroups moves groups from confirmed_groups and flagged_groups to locked_groups.
// This happens when groups are found to be locked by Roblox.
func (r *GroupModel) RemoveLockedGroups(ctx context.Context, groupIDs []uint64) error {
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Move confirmed groups to locked_groups
		var confirmedGroups []types.ConfirmedGroup
		err := tx.NewSelect().Model(&confirmedGroups).
			Where("id IN (?)", bun.In(groupIDs)).
			Scan(ctx)
		if err != nil {
			r.logger.Error("Failed to select confirmed groups for locking", zap.Error(err))
			return err
		}

		for _, group := range confirmedGroups {
			lockedGroup := &types.LockedGroup{
				Group:    group.Group,
				LockedAt: time.Now(),
			}
			_, err = tx.NewInsert().Model(lockedGroup).
				On("CONFLICT (id) DO UPDATE").
				Exec(ctx)
			if err != nil {
				r.logger.Error("Failed to insert locked group from confirmed_groups", zap.Error(err), zap.Uint64("groupID", group.ID))
				return err
			}
		}

		// Move flagged groups to locked_groups
		var flaggedGroups []types.FlaggedGroup
		err = tx.NewSelect().Model(&flaggedGroups).
			Where("id IN (?)", bun.In(groupIDs)).
			Scan(ctx)
		if err != nil {
			r.logger.Error("Failed to select flagged groups for locking", zap.Error(err))
			return err
		}

		for _, group := range flaggedGroups {
			lockedGroup := &types.LockedGroup{
				Group:    group.Group,
				LockedAt: time.Now(),
			}
			_, err = tx.NewInsert().Model(lockedGroup).
				On("CONFLICT (id) DO UPDATE").
				Exec(ctx)
			if err != nil {
				r.logger.Error("Failed to insert locked group from flagged_groups", zap.Error(err), zap.Uint64("groupID", group.ID))
				return err
			}
		}

		// Remove groups from confirmed_groups
		_, err = tx.NewDelete().Model((*types.ConfirmedGroup)(nil)).
			Where("id IN (?)", bun.In(groupIDs)).
			Exec(ctx)
		if err != nil {
			r.logger.Error("Failed to remove locked groups from confirmed_groups", zap.Error(err))
			return err
		}

		// Remove groups from flagged_groups
		_, err = tx.NewDelete().Model((*types.FlaggedGroup)(nil)).
			Where("id IN (?)", bun.In(groupIDs)).
			Exec(ctx)
		if err != nil {
			r.logger.Error("Failed to remove locked groups from flagged_groups", zap.Error(err))
			return err
		}

		r.logger.Debug("Moved locked groups to locked_groups", zap.Int("count", len(groupIDs)))
		return nil
	})
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
			Order("last_scanned ASC").
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
				r.logger.Error("Failed to update last_scanned for confirmed group", zap.Error(err))
				return err
			}
			group = &confirmedGroup.Group
			return nil
		}

		// If no confirmed groups, try flagged groups
		var flaggedGroup types.FlaggedGroup
		err = tx.NewSelect().Model(&flaggedGroup).
			Where("last_scanned < NOW() - INTERVAL '1 day'").
			Order("last_scanned ASC").
			Limit(1).
			For("UPDATE SKIP LOCKED").
			Scan(ctx)
		if err != nil {
			r.logger.Error("Failed to get group to scan", zap.Error(err))
			return err
		}

		// Update last_scanned
		_, err = tx.NewUpdate().Model(&flaggedGroup).
			Set("last_scanned = ?", time.Now()).
			Where("id = ?", flaggedGroup.ID).
			Exec(ctx)
		if err != nil {
			r.logger.Error("Failed to update last_scanned for flagged group", zap.Error(err))
			return err
		}
		group = &flaggedGroup.Group
		return nil
	})
	if err != nil {
		return nil, err
	}

	return group, nil
}

// GetGroupToReview finds a group to review based on the sort method and target mode.
func (r *GroupModel) GetGroupToReview(ctx context.Context, sortBy types.SortBy, targetMode types.ReviewTargetMode) (*types.ReviewGroup, error) {
	// Define models in priority order based on target mode
	var models []interface{}
	switch targetMode {
	case types.FlaggedReviewTarget:
		models = []interface{}{
			&types.FlaggedGroup{},   // Primary target
			&types.ConfirmedGroup{}, // First fallback
			&types.ClearedGroup{},   // Second fallback
			&types.LockedGroup{},    // Last fallback
		}
	case types.ConfirmedReviewTarget:
		models = []interface{}{
			&types.ConfirmedGroup{}, // Primary target
			&types.FlaggedGroup{},   // First fallback
			&types.ClearedGroup{},   // Second fallback
			&types.LockedGroup{},    // Last fallback
		}
	case types.ClearedReviewTarget:
		models = []interface{}{
			&types.ClearedGroup{},   // Primary target
			&types.FlaggedGroup{},   // First fallback
			&types.ConfirmedGroup{}, // Second fallback
			&types.LockedGroup{},    // Last fallback
		}
	case types.BannedReviewTarget:
		models = []interface{}{
			&types.LockedGroup{},    // Primary target
			&types.FlaggedGroup{},   // First fallback
			&types.ConfirmedGroup{}, // Second fallback
			&types.ClearedGroup{},   // Last fallback
		}
	}

	// Try each model in order until we find a group
	for _, model := range models {
		result, err := r.getNextToReview(ctx, model, sortBy)
		if err == nil {
			return r.convertToReviewGroup(result)
		}
	}

	return nil, types.ErrNoGroupsToReview
}

// convertToReviewGroup converts any group type to a ReviewGroup.
func (r *GroupModel) convertToReviewGroup(group interface{}) (*types.ReviewGroup, error) {
	review := &types.ReviewGroup{}

	switch g := group.(type) {
	case *types.FlaggedGroup:
		review.Group = g.Group
		review.Status = types.GroupTypeFlagged
	case *types.ConfirmedGroup:
		review.Group = g.Group
		review.VerifiedAt = g.VerifiedAt
		review.Status = types.GroupTypeConfirmed
	case *types.ClearedGroup:
		review.Group = g.Group
		review.ClearedAt = g.ClearedAt
		review.Status = types.GroupTypeCleared
	case *types.LockedGroup:
		review.Group = g.Group
		review.LockedAt = g.LockedAt
		review.Status = types.GroupTypeLocked
	default:
		return nil, fmt.Errorf("%w: %T", types.ErrUnsupportedModel, group)
	}

	return review, nil
}

// getNextToReview handles the common logic for getting the next item to review.
func (r *GroupModel) getNextToReview(ctx context.Context, model interface{}, sortBy types.SortBy) (interface{}, error) {
	var result interface{}
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		query := tx.NewSelect().
			Model(model).
			Where("last_viewed < NOW() - INTERVAL '10 minutes'")

		// Apply sort order
		switch sortBy {
		case types.SortByConfidence:
			query.Order("confidence DESC")
		case types.SortByReputation:
			query.Order("reputation ASC")
		case types.SortByRandom:
			query.OrderExpr("RANDOM()")
		default:
			return fmt.Errorf("%w: %s", types.ErrInvalidSortBy, sortBy)
		} //exhaustive:ignore

		err := query.Limit(1).
			For("UPDATE SKIP LOCKED").
			Scan(ctx)
		if err != nil {
			return err
		}

		// Update last_viewed based on model type
		now := time.Now()
		var id uint64
		switch m := model.(type) {
		case *types.FlaggedGroup:
			m.LastViewed = now
			id = m.ID
			result = m
		case *types.ConfirmedGroup:
			m.LastViewed = now
			id = m.ID
			result = m
		case *types.ClearedGroup:
			m.LastViewed = now
			id = m.ID
			result = m
		case *types.LockedGroup:
			m.LastViewed = now
			id = m.ID
			result = m
		default:
			return fmt.Errorf("%w: %T", types.ErrUnsupportedModel, model)
		}

		_, err = tx.NewUpdate().
			Model(model).
			Set("last_viewed = ?", now).
			Where("id = ?", id).
			Exec(ctx)
		if err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return result, nil
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
			r.logger.Error("Failed to check confirmed groups", zap.Error(err))
			return err
		}

		return nil
	})

	return confirmedGroupIDs, err
}

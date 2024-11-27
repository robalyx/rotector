package models

import (
	"context"
	"fmt"
	"time"

	"github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/uptrace/bun"
	"go.uber.org/zap"
)

// Group combines all the information needed to review a group.
type Group struct {
	ID             uint64            `bun:",pk"           json:"id"`
	Name           string            `bun:",notnull"      json:"name"`
	Description    string            `bun:",notnull"      json:"description"`
	Owner          uint64            `bun:",notnull"      json:"owner"`
	Shout          *types.GroupShout `bun:"type:jsonb"    json:"shout"`
	MemberCount    uint64            `bun:",notnull"      json:"memberCount"`
	Reason         string            `bun:",notnull"      json:"reason"`
	Confidence     float64           `bun:",notnull"      json:"confidence"`
	LastScanned    time.Time         `bun:",notnull"      json:"lastScanned"`
	LastUpdated    time.Time         `bun:",notnull"      json:"lastUpdated"`
	LastViewed     time.Time         `bun:",notnull"      json:"lastViewed"`
	LastPurgeCheck time.Time         `bun:",notnull"      json:"lastPurgeCheck"`
	ThumbnailURL   string            `bun:",notnull"      json:"thumbnailUrl"`
	Upvotes        int               `bun:",notnull"      json:"upvotes"`
	Downvotes      int               `bun:",notnull"      json:"downvotes"`
	Reputation     int               `bun:",notnull"      json:"reputation"`
	FlaggedUsers   []uint64          `bun:"type:bigint[]" json:"flaggedUsers"`
}

// FlaggedGroup extends Group to track groups that need review.
type FlaggedGroup struct {
	Group
}

// ConfirmedGroup extends Group to track groups that have been reviewed and confirmed.
type ConfirmedGroup struct {
	Group
	VerifiedAt time.Time `bun:",notnull" json:"verifiedAt"`
}

// ClearedGroup extends Group to track groups that were cleared during review.
// The ClearedAt field shows when the group was cleared by a moderator.
type ClearedGroup struct {
	Group
	ClearedAt time.Time `bun:",notnull" json:"clearedAt"`
}

// LockedGroup extends Group to track groups that were locked and removed.
// The LockedAt field shows when the group was found to be locked.
type LockedGroup struct {
	Group
	LockedAt time.Time `bun:",notnull" json:"lockedAt"`
}

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

// CheckConfirmedGroups finds which groups from a list of IDs exist in confirmed_groups.
// Returns a slice of confirmed group IDs.
func (r *GroupModel) CheckConfirmedGroups(ctx context.Context, groupIDs []uint64) ([]uint64, error) {
	var confirmedGroupIDs []uint64
	err := r.db.NewSelect().
		Model((*ConfirmedGroup)(nil)).
		Column("id").
		Where("id IN (?)", bun.In(groupIDs)).
		Scan(ctx, &confirmedGroupIDs)
	if err != nil {
		r.logger.Error("Failed to check confirmed groups", zap.Error(err))
		return nil, err
	}

	r.logger.Debug("Checked confirmed groups",
		zap.Int("total", len(groupIDs)),
		zap.Int("confirmed", len(confirmedGroupIDs)))

	return confirmedGroupIDs, nil
}

// SaveFlaggedGroups adds or updates groups in the flagged_groups table.
// For each group, it updates all fields if the group already exists,
// or inserts a new record if they don't.
func (r *GroupModel) SaveFlaggedGroups(ctx context.Context, flaggedGroups []*FlaggedGroup) {
	r.logger.Debug("Saving flagged groups", zap.Int("count", len(flaggedGroups)))

	for _, flaggedGroup := range flaggedGroups {
		_, err := r.db.NewInsert().Model(flaggedGroup).
			On("CONFLICT (id) DO UPDATE").
			Set("name = EXCLUDED.name").
			Set("description = EXCLUDED.description").
			Set("owner = EXCLUDED.owner").
			Set("shout = EXCLUDED.shout").
			Set("member_count = EXCLUDED.member_count").
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
			Set("flagged_users = EXCLUDED.flagged_users").
			Exec(ctx)
		if err != nil {
			r.logger.Error("Error saving flagged group",
				zap.Uint64("groupID", flaggedGroup.ID),
				zap.String("name", flaggedGroup.Name),
				zap.String("reason", flaggedGroup.Reason),
				zap.Float64("confidence", flaggedGroup.Confidence),
				zap.Error(err))
			continue
		}

		r.logger.Debug("Saved flagged group",
			zap.Uint64("groupID", flaggedGroup.ID),
			zap.String("name", flaggedGroup.Name),
			zap.String("reason", flaggedGroup.Reason),
			zap.Float64("confidence", flaggedGroup.Confidence),
			zap.Time("last_updated", flaggedGroup.LastUpdated),
			zap.String("thumbnail_url", flaggedGroup.ThumbnailURL),
			zap.Uint64("member_count", flaggedGroup.MemberCount))
	}

	r.logger.Debug("Finished saving flagged groups")
}

// ConfirmGroup moves a group from flagged_groups to confirmed_groups.
// This happens when a moderator confirms that a group is inappropriate.
func (r *GroupModel) ConfirmGroup(ctx context.Context, group *FlaggedGroup) error {
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		confirmedGroup := &ConfirmedGroup{
			Group: Group{
				ID:           group.ID,
				Name:         group.Name,
				Description:  group.Description,
				Owner:        group.Owner,
				Reason:       group.Reason,
				Confidence:   group.Confidence,
				LastScanned:  group.LastScanned,
				LastUpdated:  group.LastUpdated,
				LastViewed:   group.LastViewed,
				ThumbnailURL: group.ThumbnailURL,
				Upvotes:      group.Upvotes,
				Downvotes:    group.Downvotes,
				Reputation:   group.Reputation,
			},
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

		_, err = tx.NewDelete().Model((*FlaggedGroup)(nil)).
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

// GetFlaggedGroupToReview finds a group to review based on the sort method:
// - random: selects any unviewed group randomly
// - members: selects the group with the most confirmed members.
func (r *GroupModel) GetFlaggedGroupToReview(ctx context.Context, sortBy string) (*FlaggedGroup, error) {
	var group FlaggedGroup
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		query := tx.NewSelect().Model(&group).
			Where("last_viewed IS NULL OR last_viewed < NOW() - INTERVAL '10 minutes'")

		switch sortBy {
		case SortByFlaggedUsers:
			// Order by the length of the flagged_users array
			query.OrderExpr("array_length(flagged_users, 1) DESC")
		case SortByConfidence:
			query.OrderExpr("confidence DESC")
		case SortByReputation:
			query.Order("reputation ASC")
		case SortByRandom:
			query.OrderExpr("RANDOM()")
		default:
			return fmt.Errorf("%w: %s", ErrInvalidSortBy, sortBy)
		}

		err := query.Limit(1).
			For("UPDATE SKIP LOCKED").
			Scan(ctx)
		if err != nil {
			r.logger.Error("Failed to get flagged group to review", zap.Error(err))
			return err
		}

		// Update last_viewed
		now := time.Now()
		_, err = tx.NewUpdate().Model(&group).
			Set("last_viewed = ?", now).
			Where("id = ?", group.ID).
			Exec(ctx)
		if err != nil {
			r.logger.Error("Failed to update last_viewed", zap.Error(err))
			return err
		}
		group.LastViewed = now

		return nil
	})
	if err != nil {
		return nil, err
	}

	return &group, nil
}

// ClearGroup moves a group from flagged_groups to cleared_groups.
// This happens when a moderator determines that a group was incorrectly flagged.
func (r *GroupModel) ClearGroup(ctx context.Context, group *FlaggedGroup) error {
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		clearedGroup := &ClearedGroup{
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

		_, err = tx.NewDelete().Model((*FlaggedGroup)(nil)).Where("id = ?", group.ID).Exec(ctx)
		if err != nil {
			r.logger.Error("Failed to delete group from flagged_groups", zap.Error(err), zap.Uint64("groupID", group.ID))
			return err
		}

		r.logger.Debug("Group cleared and moved to cleared_groups", zap.Uint64("groupID", group.ID))

		return nil
	})
}

// GetClearedGroupByID finds a group in the cleared_groups table by their ID.
func (r *GroupModel) GetClearedGroupByID(ctx context.Context, id uint64) (*ClearedGroup, error) {
	var group ClearedGroup
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
		Model((*ClearedGroup)(nil)).
		Count(ctx)
	if err != nil {
		r.logger.Error("Failed to get cleared groups count", zap.Error(err))
		return 0, err
	}
	return count, nil
}

// UpdateTrainingVotes updates the upvotes or downvotes count for a group in training mode.
func (r *GroupModel) UpdateTrainingVotes(ctx context.Context, groupID uint64, isUpvote bool) error {
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var group FlaggedGroup
		err := tx.NewSelect().
			Model(&group).
			Column("upvotes", "downvotes").
			Where("id = ?", groupID).
			Scan(ctx)
		if err != nil {
			return err
		}

		if isUpvote {
			group.Upvotes++
		} else {
			group.Downvotes++
		}

		newReputation := group.Upvotes - group.Downvotes

		_, err = tx.NewUpdate().
			Model((*FlaggedGroup)(nil)).
			Set("upvotes = ?", group.Upvotes).
			Set("downvotes = ?", group.Downvotes).
			Set("reputation = ?", newReputation).
			Where("id = ?", groupID).
			Exec(ctx)
		if err != nil {
			r.logger.Error("Failed to update training votes",
				zap.Error(err),
				zap.Uint64("groupID", groupID),
				zap.String("voteType", map[bool]string{true: "upvote", false: "downvote"}[isUpvote]))
			return err
		}

		return nil
	})

	return err
}

// GetGroupsToCheck finds groups that haven't been checked for locked status recently.
func (r *GroupModel) GetGroupsToCheck(ctx context.Context, limit int) ([]uint64, error) {
	var groupIDs []uint64

	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Use CTE to select groups
		err := tx.NewRaw(`
			WITH selected_groups AS (
				(
					SELECT id, 'confirmed' as type
					FROM confirmed_groups
					WHERE last_purge_check IS NULL 
						OR last_purge_check < NOW() - INTERVAL '8 hours'
					ORDER BY RANDOM()
					LIMIT ?
				)
				UNION ALL
				(
					SELECT id, 'flagged' as type
					FROM flagged_groups
					WHERE last_purge_check IS NULL 
						OR last_purge_check < NOW() - INTERVAL '8 hours'
					ORDER BY RANDOM()
					LIMIT ?
				)
			)
			SELECT id FROM selected_groups
		`, limit/2, limit/2).Scan(ctx, &groupIDs)
		if err != nil {
			r.logger.Error("Failed to get groups to check", zap.Error(err))
			return err
		}

		// Update last_purge_check for selected groups
		if len(groupIDs) > 0 {
			_, err = tx.NewUpdate().Model((*ConfirmedGroup)(nil)).
				Set("last_purge_check = ?", time.Now()).
				Where("id IN (?)", bun.In(groupIDs)).
				Exec(ctx)
			if err != nil {
				r.logger.Error("Failed to update last_purge_check for confirmed groups", zap.Error(err))
				return err
			}

			_, err = tx.NewUpdate().Model((*FlaggedGroup)(nil)).
				Set("last_purge_check = ?", time.Now()).
				Where("id IN (?)", bun.In(groupIDs)).
				Exec(ctx)
			if err != nil {
				r.logger.Error("Failed to update last_purge_check for flagged groups", zap.Error(err))
				return err
			}
		}

		return nil
	})

	return groupIDs, err
}

// PurgeOldClearedGroups removes cleared groups older than the cutoff date.
// This helps maintain database size by removing groups that were cleared long ago.
func (r *GroupModel) PurgeOldClearedGroups(ctx context.Context, cutoffDate time.Time) (int, error) {
	result, err := r.db.NewDelete().
		Model((*ClearedGroup)(nil)).
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
		var confirmedGroups []ConfirmedGroup
		err := tx.NewSelect().Model(&confirmedGroups).
			Where("id IN (?)", bun.In(groupIDs)).
			Scan(ctx)
		if err != nil {
			r.logger.Error("Failed to select confirmed groups for locking", zap.Error(err))
			return err
		}

		for _, group := range confirmedGroups {
			lockedGroup := &LockedGroup{
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
		var flaggedGroups []FlaggedGroup
		err = tx.NewSelect().Model(&flaggedGroups).
			Where("id IN (?)", bun.In(groupIDs)).
			Scan(ctx)
		if err != nil {
			r.logger.Error("Failed to select flagged groups for locking", zap.Error(err))
			return err
		}

		for _, group := range flaggedGroups {
			lockedGroup := &LockedGroup{
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
		_, err = tx.NewDelete().Model((*ConfirmedGroup)(nil)).
			Where("id IN (?)", bun.In(groupIDs)).
			Exec(ctx)
		if err != nil {
			r.logger.Error("Failed to remove locked groups from confirmed_groups", zap.Error(err))
			return err
		}

		// Remove groups from flagged_groups
		_, err = tx.NewDelete().Model((*FlaggedGroup)(nil)).
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
func (r *GroupModel) GetGroupToScan(ctx context.Context) (*Group, error) {
	var group *Group
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// First try confirmed groups
		var confirmedGroup ConfirmedGroup
		err := tx.NewSelect().Model(&confirmedGroup).
			Where("last_scanned IS NULL OR last_scanned < NOW() - INTERVAL '1 day'").
			Order("last_scanned ASC NULLS FIRST").
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
		var flaggedGroup FlaggedGroup
		err = tx.NewSelect().Model(&flaggedGroup).
			Where("last_scanned IS NULL OR last_scanned < NOW() - INTERVAL '1 day'").
			Order("last_scanned ASC NULLS FIRST").
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

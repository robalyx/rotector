package models

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/database/types/enum"
	"github.com/uptrace/bun"
	"go.uber.org/zap"
)

// GroupModel handles database operations for group records.
type GroupModel struct {
	db     *bun.DB
	logger *zap.Logger
}

// NewGroup creates a GroupModel.
func NewGroup(db *bun.DB, logger *zap.Logger) *GroupModel {
	return &GroupModel{
		db:     db,
		logger: logger.Named("db_group"),
	}
}

// SaveGroups saves groups to the database.
//
// Deprecated: Use Service().Group().SaveGroups() instead.
func (r *GroupModel) SaveGroups(ctx context.Context, tx bun.Tx, groups []*types.ReviewGroup) error {
	if len(groups) == 0 {
		return nil
	}

	// Extract base groups
	baseGroups := make([]*types.Group, len(groups))
	for i, group := range groups {
		baseGroups[i] = group.Group
	}

	// Update groups table
	_, err := tx.NewInsert().
		Model(&baseGroups).
		On("CONFLICT (id) DO UPDATE").
		Set("uuid = EXCLUDED.uuid").
		Set("name = EXCLUDED.name").
		Set("description = EXCLUDED.description").
		Set("owner = EXCLUDED.owner").
		Set("shout = EXCLUDED.shout").
		Set("confidence = EXCLUDED.confidence").
		Set("status = EXCLUDED.status").
		Set("last_scanned = EXCLUDED.last_scanned").
		Set("last_updated = EXCLUDED.last_updated").
		Set("last_viewed = EXCLUDED.last_viewed").
		Set("last_lock_check = EXCLUDED.last_lock_check").
		Set("is_locked = EXCLUDED.is_locked").
		Set("is_deleted = EXCLUDED.is_deleted").
		Set("thumbnail_url = EXCLUDED.thumbnail_url").
		Set("last_thumbnail_update = EXCLUDED.last_thumbnail_update").
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to upsert groups: %w", err)
	}

	// Save group reasons
	var reasons []*types.GroupReason
	for _, group := range groups {
		if group.Reasons != nil {
			for reasonType, reason := range group.Reasons {
				reasons = append(reasons, &types.GroupReason{
					GroupID:    group.ID,
					ReasonType: reasonType,
					Message:    reason.Message,
					Confidence: reason.Confidence,
					Evidence:   reason.Evidence,
					CreatedAt:  time.Now(),
				})
			}
		}
	}

	if len(reasons) > 0 {
		_, err = tx.NewInsert().
			Model(&reasons).
			On("CONFLICT (group_id, reason_type) DO UPDATE").
			Set("message = EXCLUDED.message").
			Set("confidence = EXCLUDED.confidence").
			Set("evidence = EXCLUDED.evidence").
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to upsert group reasons: %w", err)
		}
	}

	return nil
}

// ConfirmGroup moves a group to confirmed status and creates a verification record.
//
// Deprecated: Use Service().Group().ConfirmGroup() instead.
func (r *GroupModel) ConfirmGroup(ctx context.Context, group *types.ReviewGroup) error {
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Update group status
		_, err := tx.NewUpdate().
			Model(group.Group).
			Set("status = ?", enum.GroupTypeConfirmed).
			Where("id = ?", group.ID).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to update group status: %w", err)
		}

		// Create verification record
		verification := &types.GroupVerification{
			GroupID:    group.ID,
			ReviewerID: group.ReviewerID,
			VerifiedAt: time.Now(),
		}
		_, err = tx.NewInsert().
			Model(verification).
			On("CONFLICT (group_id) DO UPDATE").
			Set("reviewer_id = EXCLUDED.reviewer_id").
			Set("verified_at = EXCLUDED.verified_at").
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to create verification record: %w", err)
		}

		// Save reasons if any exist
		if group.Reasons != nil {
			var reasons []*types.GroupReason
			for reasonType, reason := range group.Reasons {
				reasons = append(reasons, &types.GroupReason{
					GroupID:    group.ID,
					ReasonType: reasonType,
					Message:    reason.Message,
					Confidence: reason.Confidence,
					Evidence:   reason.Evidence,
					CreatedAt:  time.Now(),
				})
			}

			if len(reasons) > 0 {
				_, err = tx.NewInsert().
					Model(&reasons).
					On("CONFLICT (group_id, reason_type) DO UPDATE").
					Set("message = EXCLUDED.message").
					Set("confidence = EXCLUDED.confidence").
					Set("evidence = EXCLUDED.evidence").
					Exec(ctx)
				if err != nil {
					return fmt.Errorf("failed to update group reasons: %w", err)
				}
			}
		}

		return nil
	})
}

// ClearGroup moves a group to cleared status and creates a clearance record.
//
// Deprecated: Use Service().Group().ClearGroup() instead.
func (r *GroupModel) ClearGroup(ctx context.Context, group *types.ReviewGroup) error {
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Update group status
		_, err := tx.NewUpdate().
			Model(group.Group).
			Set("status = ?", enum.GroupTypeCleared).
			Where("id = ?", group.ID).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to update group status: %w", err)
		}

		// Create clearance record
		clearance := &types.GroupClearance{
			GroupID:    group.ID,
			ReviewerID: group.ReviewerID,
			ClearedAt:  time.Now(),
		}
		_, err = tx.NewInsert().
			Model(clearance).
			On("CONFLICT (group_id) DO UPDATE").
			Set("reviewer_id = EXCLUDED.reviewer_id").
			Set("cleared_at = EXCLUDED.cleared_at").
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to create clearance record: %w", err)
		}

		// Save reasons if any exist
		if group.Reasons != nil {
			var reasons []*types.GroupReason
			for reasonType, reason := range group.Reasons {
				reasons = append(reasons, &types.GroupReason{
					GroupID:    group.ID,
					ReasonType: reasonType,
					Message:    reason.Message,
					Confidence: reason.Confidence,
					Evidence:   reason.Evidence,
					CreatedAt:  time.Now(),
				})
			}

			if len(reasons) > 0 {
				_, err = tx.NewInsert().
					Model(&reasons).
					On("CONFLICT (group_id, reason_type) DO UPDATE").
					Set("message = EXCLUDED.message").
					Set("confidence = EXCLUDED.confidence").
					Set("evidence = EXCLUDED.evidence").
					Exec(ctx)
				if err != nil {
					return fmt.Errorf("failed to update group reasons: %w", err)
				}
			}
		}

		return nil
	})
}

// GetGroupByID retrieves a group by either their numeric ID or UUID.
//
// Deprecated: Use Service().Group().GetGroupByID() instead.
func (r *GroupModel) GetGroupByID(
	ctx context.Context, groupID string, fields types.GroupField,
) (*types.ReviewGroup, error) {
	var group types.Group
	var result types.ReviewGroup

	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Build query
		query := tx.NewSelect().
			Model(&group).
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

		// Get group
		err := query.Scan(ctx)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return types.ErrGroupNotFound
			}
			return fmt.Errorf("failed to get group: %w", err)
		}

		// Set base group data
		result.Group = &group

		// Get group reasons
		var reasons []*types.GroupReason
		err = tx.NewSelect().
			Model(&reasons).
			Where("group_id = ?", group.ID).
			Scan(ctx)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("failed to get group reasons: %w", err)
		}

		// Convert reasons to map
		result.Reasons = make(types.Reasons[enum.GroupReasonType])
		for _, reason := range reasons {
			result.Reasons[reason.ReasonType] = &types.Reason{
				Message:    reason.Message,
				Confidence: reason.Confidence,
				Evidence:   reason.Evidence,
			}
		}

		// Get verification data if confirmed
		switch group.Status {
		case enum.GroupTypeConfirmed:
			var verification types.GroupVerification
			err = tx.NewSelect().
				Model(&verification).
				Where("group_id = ?", group.ID).
				Scan(ctx)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("failed to get verification data: %w", err)
			}
			if err == nil {
				result.ReviewerID = verification.ReviewerID
				result.VerifiedAt = verification.VerifiedAt
			}
		case enum.GroupTypeCleared:
			var clearance types.GroupClearance
			err = tx.NewSelect().
				Model(&clearance).
				Where("group_id = ?", group.ID).
				Scan(ctx)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("failed to get clearance data: %w", err)
			}
			if err == nil {
				result.ReviewerID = clearance.ReviewerID
				result.ClearedAt = clearance.ClearedAt
			}
		case enum.GroupTypeFlagged:
			// Nothing to do here
		}

		// Update last_viewed
		_, err = tx.NewUpdate().
			Model(group).
			Set("last_viewed = ?", time.Now()).
			Where("id = ?", group.ID).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to update last_viewed: %w", err)
		}

		return nil
	})

	return &result, err
}

// GetGroupsByIDs retrieves specified group information for a list of group IDs.
// Returns a map of group IDs to review groups.
func (r *GroupModel) GetGroupsByIDs(
	ctx context.Context, groupIDs []uint64, fields types.GroupField,
) (map[uint64]*types.ReviewGroup, error) {
	groups := make(map[uint64]*types.ReviewGroup)

	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Query all groups
		var baseGroups []types.Group
		err := tx.NewSelect().
			Model(&baseGroups).
			Column(fields.Columns()...).
			Where("id IN (?)", bun.In(groupIDs)).
			Scan(ctx)
		if err != nil {
			return fmt.Errorf("failed to get groups: %w", err)
		}

		// Get verifications and clearances
		var verifications []types.GroupVerification
		err = tx.NewSelect().
			Model(&verifications).
			Where("group_id IN (?)", bun.In(groupIDs)).
			Scan(ctx)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("failed to get verifications: %w", err)
		}

		var clearances []types.GroupClearance
		err = tx.NewSelect().
			Model(&clearances).
			Where("group_id IN (?)", bun.In(groupIDs)).
			Scan(ctx)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("failed to get clearances: %w", err)
		}

		// Get group reasons
		var reasons []*types.GroupReason
		err = tx.NewSelect().
			Model(&reasons).
			Where("group_id IN (?)", bun.In(groupIDs)).
			Scan(ctx)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("failed to get group reasons: %w", err)
		}

		// Map verifications and clearances by group ID
		verificationMap := make(map[uint64]types.GroupVerification)
		for _, v := range verifications {
			verificationMap[v.GroupID] = v
		}

		clearanceMap := make(map[uint64]types.GroupClearance)
		for _, c := range clearances {
			clearanceMap[c.GroupID] = c
		}

		// Map reasons by group ID
		reasonMap := make(map[uint64]types.Reasons[enum.GroupReasonType])
		for _, reason := range reasons {
			if _, ok := reasonMap[reason.GroupID]; !ok {
				reasonMap[reason.GroupID] = make(types.Reasons[enum.GroupReasonType])
			}
			reasonMap[reason.GroupID][reason.ReasonType] = &types.Reason{
				Message:    reason.Message,
				Confidence: reason.Confidence,
				Evidence:   reason.Evidence,
			}
		}

		// Build review groups
		for _, group := range baseGroups {
			reviewGroup := &types.ReviewGroup{
				Group:   &group,
				Reasons: reasonMap[group.ID],
			}

			if v, ok := verificationMap[group.ID]; ok {
				reviewGroup.ReviewerID = v.ReviewerID
				reviewGroup.VerifiedAt = v.VerifiedAt
			}

			if c, ok := clearanceMap[group.ID]; ok {
				reviewGroup.ReviewerID = c.ReviewerID
				reviewGroup.ClearedAt = c.ClearedAt
			}

			groups[group.ID] = reviewGroup
		}

		return nil
	})

	return groups, err
}

// GetFlaggedAndConfirmedGroups retrieves all flagged and confirmed groups.
func (r *GroupModel) GetFlaggedAndConfirmedGroups(ctx context.Context) ([]*types.ReviewGroup, error) {
	// Get groups
	var groups []types.Group
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		err := tx.NewSelect().
			Model(&groups).
			Column("id", "reasons", "confidence").
			Where("status IN (?)", bun.In([]enum.GroupType{enum.GroupTypeFlagged, enum.GroupTypeConfirmed})).
			Scan(ctx)
		if err != nil {
			return fmt.Errorf("failed to get groups: %w", err)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	// Convert to review groups
	result := make([]*types.ReviewGroup, len(groups))
	for i, group := range groups {
		result[i] = &types.ReviewGroup{
			Group: &group,
		}
	}

	return result, nil
}

// GetGroupsToCheck finds groups that haven't been checked for locked status recently.
func (r *GroupModel) GetGroupsToCheck(
	ctx context.Context, limit int,
) (groupIDs []uint64, lockedIDs []uint64, err error) {
	var groups []types.Group
	err = r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Get groups that need checking
		err := tx.NewSelect().
			Model(&groups).
			Column("id", "is_locked").
			Where("last_lock_check < NOW() - INTERVAL '1 day'").
			Where("status IN (?)", bun.In([]enum.GroupType{enum.GroupTypeFlagged, enum.GroupTypeConfirmed})).
			OrderExpr("last_lock_check ASC").
			Limit(limit).
			For("UPDATE SKIP LOCKED").
			Scan(ctx)
		if err != nil {
			return fmt.Errorf("failed to get groups: %w", err)
		}

		if len(groups) > 0 {
			groupIDs = make([]uint64, 0, len(groups))
			for _, group := range groups {
				groupIDs = append(groupIDs, group.ID)
				if group.IsLocked {
					lockedIDs = append(lockedIDs, group.ID)
				}
			}

			// Update last_lock_check
			_, err = tx.NewUpdate().
				Model(&groups).
				Set("last_lock_check = NOW()").
				Where("id IN (?)", bun.In(groupIDs)).
				Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to update groups: %w", err)
			}
		}

		return nil
	})

	return groupIDs, lockedIDs, err
}

// MarkGroupsLockStatus updates the locked status of groups.
func (r *GroupModel) MarkGroupsLockStatus(ctx context.Context, groupIDs []uint64, isLocked bool) error {
	_, err := r.db.NewUpdate().
		Model((*types.Group)(nil)).
		Set("is_locked = ?", isLocked).
		Where("id IN (?)", bun.In(groupIDs)).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to mark groups lock status: %w", err)
	}

	r.logger.Debug("Marked groups lock status",
		zap.Int("count", len(groupIDs)),
		zap.Bool("isLocked", isLocked))
	return nil
}

// GetLockedCount returns the total number of locked groups.
func (r *GroupModel) GetLockedCount(ctx context.Context) (int, error) {
	count, err := r.db.NewSelect().
		Model((*types.Group)(nil)).
		Where("is_locked = true").
		Where("status IN (?, ?)", enum.GroupTypeFlagged, enum.GroupTypeConfirmed).
		Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get locked groups count: %w", err)
	}
	return count, nil
}

// GetGroupCounts returns counts for all group statuses.
func (r *GroupModel) GetGroupCounts(ctx context.Context) (*types.GroupCounts, error) {
	var counts types.GroupCounts
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Get counts by status
		var statusCounts []struct {
			Status enum.GroupType `bun:"status"`
			Count  int            `bun:"count"`
		}
		err := tx.NewSelect().
			Model((*types.Group)(nil)).
			Column("status").
			ColumnExpr("COUNT(*) AS count").
			GroupExpr("status").
			Scan(ctx, &statusCounts)
		if err != nil {
			return fmt.Errorf("failed to get group counts: %w", err)
		}

		// Map counts to their respective fields
		for _, sc := range statusCounts {
			switch sc.Status {
			case enum.GroupTypeConfirmed:
				counts.Confirmed = sc.Count
			case enum.GroupTypeFlagged:
				counts.Flagged = sc.Count
			case enum.GroupTypeCleared:
				counts.Cleared = sc.Count
			}
		}

		// Get locked count
		lockedCount, err := r.GetLockedCount(ctx)
		if err != nil {
			return err
		}
		counts.Locked = lockedCount

		return nil
	})
	if err != nil {
		return nil, err
	}

	return &counts, nil
}

// PurgeOldClearedGroups removes cleared groups older than the cutoff date.
func (r *GroupModel) PurgeOldClearedGroups(ctx context.Context, cutoffDate time.Time) (int, error) {
	var clearances []types.GroupClearance
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Get groups to delete
		err := tx.NewSelect().
			Model(&clearances).
			Column("group_id").
			Where("cleared_at < ?", cutoffDate).
			Scan(ctx)
		if err != nil {
			return fmt.Errorf("failed to get groups to delete: %w", err)
		}

		if len(clearances) > 0 {
			groupIDs := make([]uint64, len(clearances))
			for i, c := range clearances {
				groupIDs[i] = c.GroupID
			}

			// Delete groups
			_, err = tx.NewDelete().
				Model((*types.Group)(nil)).
				Where("id IN (?)", bun.In(groupIDs)).
				Where("status = ?", enum.GroupTypeCleared).
				Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to delete groups: %w", err)
			}

			// Delete clearance records
			_, err = tx.NewDelete().
				Model(&clearances).
				Where("group_id IN (?)", bun.In(groupIDs)).
				Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to delete clearance records: %w", err)
			}
		}

		return nil
	})
	if err != nil {
		return 0, err
	}

	r.logger.Debug("Purged old cleared groups",
		zap.Int("count", len(clearances)),
		zap.Time("cutoffDate", cutoffDate))

	return len(clearances), nil
}

// GetGroupsForThumbnailUpdate retrieves groups that need thumbnail updates.
func (r *GroupModel) GetGroupsForThumbnailUpdate(ctx context.Context, limit int) (map[uint64]*types.ReviewGroup, error) {
	groups := make(map[uint64]*types.ReviewGroup)
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var baseGroups []types.Group
		err := tx.NewSelect().
			Model(&baseGroups).
			Where("last_thumbnail_update < NOW() - INTERVAL '7 days'").
			Where("is_deleted = false").
			OrderExpr("last_thumbnail_update ASC").
			Limit(limit).
			Scan(ctx)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("failed to query groups for thumbnail update: %w", err)
		}

		for _, group := range baseGroups {
			groups[group.ID] = &types.ReviewGroup{
				Group: &group,
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return groups, nil
}

// DeleteGroup removes a group and all associated data from the database.
func (r *GroupModel) DeleteGroup(ctx context.Context, groupID uint64) (bool, error) {
	var totalAffected int64
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Delete group reasons
		result, err := tx.NewDelete().
			Model((*types.GroupReason)(nil)).
			Where("group_id = ?", groupID).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to delete group reasons: %w", err)
		}
		affected, _ := result.RowsAffected()
		totalAffected += affected

		// Delete group
		result, err = tx.NewDelete().
			Model((*types.Group)(nil)).
			Where("id = ?", groupID).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to delete group: %w", err)
		}
		affected, _ = result.RowsAffected()
		totalAffected += affected

		// Delete verification if exists
		result, err = tx.NewDelete().
			Model((*types.GroupVerification)(nil)).
			Where("group_id = ?", groupID).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to delete verification record: %w", err)
		}
		affected, _ = result.RowsAffected()
		totalAffected += affected

		// Delete clearance if exists
		result, err = tx.NewDelete().
			Model((*types.GroupClearance)(nil)).
			Where("group_id = ?", groupID).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to delete clearance record: %w", err)
		}
		affected, _ = result.RowsAffected()
		totalAffected += affected

		return nil
	})

	return totalAffected > 0, err
}

// GetGroupToScan finds the next group to scan.
func (r *GroupModel) GetGroupToScan(ctx context.Context) (*types.Group, error) {
	var group types.Group
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Try confirmed and flagged groups
		err := tx.NewSelect().
			Model(&group).
			Where("last_scanned < NOW() - INTERVAL '1 day'").
			Where("status IN (?)", bun.In([]enum.GroupType{enum.GroupTypeFlagged, enum.GroupTypeConfirmed})).
			OrderExpr("last_scanned ASC, confidence DESC").
			Limit(1).
			For("UPDATE SKIP LOCKED").
			Scan(ctx)
		if err != nil {
			return fmt.Errorf("failed to get group to scan: %w", err)
		}

		// Update last_scanned
		_, err = tx.NewUpdate().
			Model(&group).
			Set("last_scanned = ?", time.Now()).
			Where("id = ?", group.ID).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to update last_scanned: %w", err)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return &group, nil
}

// GetNextToReview handles the common logic for getting the next item to review.
//
// Deprecated: Use Service().Group().GetGroupToReview() instead.
func (r *GroupModel) GetNextToReview(
	ctx context.Context, targetStatus enum.GroupType, sortBy enum.ReviewSortBy, recentIDs []uint64,
) (*types.ReviewGroup, error) {
	var group types.Group
	var result types.ReviewGroup

	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Build query
		query := tx.NewSelect().
			Model(&group).
			Where("status = ?", targetStatus)

		// Exclude recently reviewed IDs if any exist
		if len(recentIDs) > 0 {
			query.Where("id NOT IN (?)", bun.In(recentIDs))
		}

		// Apply sort order
		switch sortBy {
		case enum.ReviewSortByConfidence:
			query.OrderExpr("confidence DESC, last_viewed ASC")
		case enum.ReviewSortByLastUpdated:
			query.OrderExpr("last_updated ASC, last_viewed ASC")
		case enum.ReviewSortByReputation:
			query.Join("LEFT JOIN group_reputations ON group_reputations.id = groups.id").
				OrderExpr("COALESCE(group_reputations.score, 0) ASC, last_viewed ASC")
		case enum.ReviewSortByLastViewed:
			query.Order("last_viewed ASC")
		case enum.ReviewSortByRandom:
			query.OrderExpr("RANDOM()")
		}

		query.Limit(1).For("UPDATE")

		// Get group
		err := query.Scan(ctx)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return types.ErrNoGroupsToReview
			}
			return err
		}

		result.Group = &group

		// Get group reasons
		var reasons []*types.GroupReason
		err = tx.NewSelect().
			Model(&reasons).
			Where("group_id = ?", group.ID).
			Scan(ctx)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("failed to get group reasons: %w", err)
		}

		// Convert reasons to map
		result.Reasons = make(types.Reasons[enum.GroupReasonType])
		for _, reason := range reasons {
			result.Reasons[reason.ReasonType] = &types.Reason{
				Message:    reason.Message,
				Confidence: reason.Confidence,
				Evidence:   reason.Evidence,
			}
		}

		// Get verification/clearance info based on status
		switch group.Status {
		case enum.GroupTypeConfirmed:
			var verification types.GroupVerification
			err = tx.NewSelect().
				Model(&verification).
				Where("group_id = ?", group.ID).
				Scan(ctx)
			if err == nil {
				result.ReviewerID = verification.ReviewerID
				result.VerifiedAt = verification.VerifiedAt
			}
		case enum.GroupTypeCleared:
			var clearance types.GroupClearance
			err = tx.NewSelect().
				Model(&clearance).
				Where("group_id = ?", group.ID).
				Scan(ctx)
			if err == nil {
				result.ReviewerID = clearance.ReviewerID
				result.ClearedAt = clearance.ClearedAt
			}
		case enum.GroupTypeFlagged:
			// Nothing to do here
		}

		// Update last_viewed
		_, err = tx.NewUpdate().
			Model(&group).
			Set("last_viewed = ?", time.Now()).
			Where("id = ?", group.ID).
			Exec(ctx)
		if err != nil {
			return err
		}

		return nil
	})

	return &result, err
}

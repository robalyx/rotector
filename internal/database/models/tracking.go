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
			LastChecked:  now,
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

// RemoveUsersFromAllGroups removes multiple users from all group tracking records.
func (r *TrackingModel) RemoveUsersFromAllGroups(ctx context.Context, userIDs []uint64) error {
	if len(userIDs) == 0 {
		return nil
	}

	_, err := r.db.NewDelete().
		Model((*types.GroupMemberTrackingUser)(nil)).
		Where("user_id IN (?)", bun.In(userIDs)).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to remove users from group tracking: %w (userCount=%d)", err, len(userIDs))
	}

	r.logger.Debug("Removed users from all group tracking",
		zap.Int("userCount", len(userIDs)))
	return nil
}

// RemoveGroupsFromTracking removes multiple groups and their users from tracking.
func (r *TrackingModel) RemoveGroupsFromTracking(ctx context.Context, groupIDs []uint64) error {
	if len(groupIDs) == 0 {
		return nil
	}

	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Remove users from tracking
		_, err := tx.NewDelete().
			Model((*types.GroupMemberTrackingUser)(nil)).
			Where("group_id IN (?)", bun.In(groupIDs)).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to remove users from group tracking: %w", err)
		}

		// Remove groups from tracking
		_, err = tx.NewDelete().
			Model((*types.GroupMemberTracking)(nil)).
			Where("id IN (?)", bun.In(groupIDs)).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to remove groups from tracking: %w", err)
		}

		return nil
	})
}

// AddOutfitAssetsToTracking adds multiple outfits to multiple assets' tracking lists.
// The map values can contain either outfit IDs or user IDs (for current outfit).
func (r *TrackingModel) AddOutfitAssetsToTracking(ctx context.Context, assetToOutfits map[uint64][]types.TrackedID) error {
	// Create tracking entries for bulk insert
	trackings := make([]types.OutfitAssetTracking, 0, len(assetToOutfits))
	trackingOutfits := make([]types.OutfitAssetTrackingOutfit, 0)
	now := time.Now()

	for assetID, trackedIDs := range assetToOutfits {
		trackings = append(trackings, types.OutfitAssetTracking{
			ID:           assetID,
			LastAppended: now,
			LastChecked:  now,
			IsFlagged:    false,
		})

		for _, tracked := range trackedIDs {
			trackingOutfits = append(trackingOutfits, types.OutfitAssetTrackingOutfit{
				AssetID:   assetID,
				TrackedID: tracked.ID,
				IsUserID:  tracked.IsUserID,
			})
		}
	}

	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Lock the assets in a consistent order to prevent deadlocks
		assetIDs := make([]uint64, 0, len(assetToOutfits))
		for assetID := range assetToOutfits {
			assetIDs = append(assetIDs, assetID)
		}
		slices.Sort(assetIDs)

		// Lock the rows we're going to update
		var existing []types.OutfitAssetTracking
		err := tx.NewSelect().
			Model(&existing).
			Where("id IN (?)", bun.In(assetIDs)).
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
			Set("is_flagged = outfit_asset_tracking.is_flagged").
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to add tracking entries: %w", err)
		}

		_, err = tx.NewInsert().
			Model(&trackingOutfits).
			On("CONFLICT (asset_id, tracked_id) DO UPDATE").
			Set("is_user_id = EXCLUDED.is_user_id").
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to add tracking outfit entries: %w", err)
		}

		r.logger.Debug("Successfully processed outfit asset tracking updates",
			zap.Int("assetCount", len(assetToOutfits)))

		return nil
	})
}

// GetOutfitAssetTrackingsToCheck finds assets that haven't been checked recently
// with priority for assets appearing in more outfits or current outfits.
func (r *TrackingModel) GetOutfitAssetTrackingsToCheck(
	ctx context.Context, batchSize int, minOutfits int, minOutfitsOverride int,
) (map[uint64][]types.TrackedID, error) {
	result := make(map[uint64][]types.TrackedID)

	now := time.Now()
	tenMinutesAgo := now.Add(-10 * time.Minute)
	oneMinuteAgo := now.Add(-1 * time.Minute)

	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var trackings []types.OutfitAssetTracking

		// Build subquery to find the asset IDs to update
		subq := tx.NewSelect().
			Model((*types.OutfitAssetTracking)(nil)).
			Column("id").
			With("outfit_counts", tx.NewSelect().
				Model((*types.OutfitAssetTrackingOutfit)(nil)).
				Column("asset_id").
				ColumnExpr("COUNT(*) as outfit_count").
				Group("asset_id")).
			Join("JOIN outfit_counts ON outfit_asset_trackings.id = outfit_counts.asset_id").
			Where("is_flagged = FALSE").
			Where("outfit_count >= ?", minOutfits).
			Where("(last_checked < ? AND outfit_count >= ?) OR "+
				"(last_checked < ? AND outfit_count >= ? / 2)",
				tenMinutesAgo, minOutfitsOverride,
				oneMinuteAgo, minOutfitsOverride).
			OrderExpr("outfit_count DESC").
			Order("last_checked ASC").
			Limit(batchSize)

		// Update the selected assets and return their data
		err := tx.NewUpdate().
			Model(&trackings).
			Set("last_checked = ?", now).
			Where("id IN (?)", subq).
			Returning("id").
			Scan(ctx)
		if err != nil {
			return fmt.Errorf("failed to get and update asset trackings: %w", err)
		}

		// Get outfits for each asset
		if len(trackings) > 0 {
			assetIDs := make([]uint64, len(trackings))
			for i, tracking := range trackings {
				assetIDs[i] = tracking.ID
			}

			var trackingOutfits []types.OutfitAssetTrackingOutfit
			err = tx.NewSelect().
				Model(&trackingOutfits).
				Where("asset_id IN (?)", bun.In(assetIDs)).
				Scan(ctx)
			if err != nil {
				return fmt.Errorf("failed to get tracking outfits: %w", err)
			}

			// Map outfits/users to their assets
			for _, to := range trackingOutfits {
				if to.IsUserID {
					result[to.AssetID] = append(result[to.AssetID], types.NewUserID(to.TrackedID))
				} else {
					result[to.AssetID] = append(result[to.AssetID], types.NewOutfitID(to.TrackedID))
				}
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}

// RemoveUsersFromAssetTracking removes multiple users and their outfits from asset tracking.
func (r *TrackingModel) RemoveUsersFromAssetTracking(ctx context.Context, userIDs []uint64) error {
	if len(userIDs) == 0 {
		return nil
	}

	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Remove user IDs from current outfit tracking
		_, err := tx.NewDelete().
			Model((*types.OutfitAssetTrackingOutfit)(nil)).
			Where("tracked_id IN (?) AND is_user_id = TRUE", bun.In(userIDs)).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to remove users from asset tracking: %w (userCount=%d)", err, len(userIDs))
		}

		// Get outfit IDs for these users
		var outfitIDs []uint64
		err = tx.NewSelect().
			Model((*types.UserOutfit)(nil)).
			Column("outfit_id").
			Where("user_id IN (?)", bun.In(userIDs)).
			Scan(ctx, &outfitIDs)
		if err != nil {
			return fmt.Errorf("failed to get user outfit IDs: %w", err)
		}

		// Remove outfit IDs if any exist
		if len(outfitIDs) > 0 {
			_, err = tx.NewDelete().
				Model((*types.OutfitAssetTrackingOutfit)(nil)).
				Where("tracked_id IN (?) AND is_user_id = FALSE", bun.In(outfitIDs)).
				Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to remove outfits from asset tracking: %w (outfitCount=%d)", err, len(outfitIDs))
			}
		}

		return nil
	})
	if err != nil {
		return err
	}

	r.logger.Debug("Removed users and their outfits from asset tracking",
		zap.Int("userCount", len(userIDs)))
	return nil
}

// AddGamesToTracking adds multiple users to multiple games' tracking lists.
func (r *TrackingModel) AddGamesToTracking(ctx context.Context, gameToUsers map[uint64][]uint64) error {
	// Create tracking entries for bulk insert
	trackings := make([]types.GameTracking, 0, len(gameToUsers))
	trackingUsers := make([]types.GameTrackingUser, 0)
	now := time.Now()

	for gameID, userIDs := range gameToUsers {
		trackings = append(trackings, types.GameTracking{
			ID:           gameID,
			LastAppended: now,
			LastChecked:  now,
			IsFlagged:    false,
		})

		for _, userID := range userIDs {
			trackingUsers = append(trackingUsers, types.GameTrackingUser{
				GameID: gameID,
				UserID: userID,
			})
		}
	}

	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Lock the games in a consistent order to prevent deadlocks
		gameIDs := make([]uint64, 0, len(gameToUsers))
		for gameID := range gameToUsers {
			gameIDs = append(gameIDs, gameID)
		}
		slices.Sort(gameIDs)

		// Lock the rows we're going to update
		var existing []types.GameTracking
		err := tx.NewSelect().
			Model(&existing).
			Where("id IN (?)", bun.In(gameIDs)).
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
			Set("is_flagged = game_tracking.is_flagged").
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

		r.logger.Debug("Successfully processed game tracking updates",
			zap.Int("gameCount", len(gameToUsers)))

		return nil
	})
}

// GetGameTrackingsToCheck finds games that haven't been checked recently
// with priority for games with more flagged users.
func (r *TrackingModel) GetGameTrackingsToCheck(
	ctx context.Context, batchSize int, minFlaggedUsers int, minFlaggedOverride int,
) (map[uint64][]uint64, error) {
	result := make(map[uint64][]uint64)

	now := time.Now()
	tenMinutesAgo := now.Add(-10 * time.Minute)
	oneMinuteAgo := now.Add(-1 * time.Minute)

	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var trackings []types.GameTracking

		// Build subquery to find the game IDs to update
		subq := tx.NewSelect().
			Model((*types.GameTracking)(nil)).
			Column("id").
			With("user_counts", tx.NewSelect().
				Model((*types.GameTrackingUser)(nil)).
				Column("game_id").
				ColumnExpr("COUNT(*) as user_count").
				Group("game_id")).
			Join("JOIN user_counts ON game_tracking.id = user_counts.game_id").
			Where("is_flagged = FALSE").
			Where("user_count >= ?", minFlaggedUsers).
			Where("(last_checked < ? AND user_count >= ?) OR "+
				"(last_checked < ? AND user_count >= ? / 2)",
				tenMinutesAgo, minFlaggedOverride,
				oneMinuteAgo, minFlaggedOverride).
			OrderExpr("user_count DESC").
			Order("last_checked ASC").
			Limit(batchSize)

		// Update the selected games and return their data
		err := tx.NewUpdate().
			Model(&trackings).
			Set("last_checked = ?", now).
			Where("id IN (?)", subq).
			Returning("id").
			Scan(ctx)
		if err != nil {
			return fmt.Errorf("failed to get and update game trackings: %w", err)
		}

		// Get flagged users for each game
		if len(trackings) > 0 {
			gameIDs := make([]uint64, len(trackings))
			for i, tracking := range trackings {
				gameIDs[i] = tracking.ID
			}

			var trackingUsers []types.GameTrackingUser
			err = tx.NewSelect().
				Model(&trackingUsers).
				Where("game_id IN (?)", bun.In(gameIDs)).
				Scan(ctx)
			if err != nil {
				return fmt.Errorf("failed to get tracking users: %w", err)
			}

			// Map users to their games
			for _, tu := range trackingUsers {
				result[tu.GameID] = append(result[tu.GameID], tu.UserID)
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}

// RemoveUsersFromGameTracking removes multiple users from game tracking.
func (r *TrackingModel) RemoveUsersFromGameTracking(ctx context.Context, userIDs []uint64) error {
	if len(userIDs) == 0 {
		return nil
	}

	_, err := r.db.NewDelete().
		Model((*types.GameTrackingUser)(nil)).
		Where("user_id IN (?)", bun.In(userIDs)).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to remove users from game tracking: %w (userCount=%d)", err, len(userIDs))
	}

	r.logger.Debug("Removed users from game tracking",
		zap.Int("userCount", len(userIDs)))
	return nil
}

// RemoveGamesFromTracking removes multiple games and their users from tracking.
func (r *TrackingModel) RemoveGamesFromTracking(ctx context.Context, gameIDs []uint64) error {
	if len(gameIDs) == 0 {
		return nil
	}

	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Remove users from tracking
		_, err := tx.NewDelete().
			Model((*types.GameTrackingUser)(nil)).
			Where("game_id IN (?)", bun.In(gameIDs)).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to remove users from game tracking: %w", err)
		}

		// Remove games from tracking
		_, err = tx.NewDelete().
			Model((*types.GameTracking)(nil)).
			Where("id IN (?)", bun.In(gameIDs)).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to remove games from tracking: %w", err)
		}

		return nil
	})
}

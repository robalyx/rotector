package models

import (
	"context"
	"fmt"
	"time"

	"github.com/rotector/rotector/internal/common/storage/database/types"
	"github.com/uptrace/bun"
	"go.uber.org/zap"
)

// UserModel handles database operations for user records.
type UserModel struct {
	db       *bun.DB
	tracking *TrackingModel
	logger   *zap.Logger
}

// NewUser creates a UserModel with references to the tracking system.
func NewUser(db *bun.DB, tracking *TrackingModel, logger *zap.Logger) *UserModel {
	return &UserModel{
		db:       db,
		tracking: tracking,
		logger:   logger,
	}
}

// SaveFlaggedUsers adds or updates users in the flagged_users table.
// For each user, it updates all fields if the user already exists,
// or inserts a new record if they don't.
func (r *UserModel) SaveFlaggedUsers(ctx context.Context, flaggedUsers map[uint64]*types.User) error {
	// Convert map to slice for bulk insert
	users := make([]*types.FlaggedUser, 0, len(flaggedUsers))
	for _, user := range flaggedUsers {
		users = append(users, &types.FlaggedUser{
			User: *user,
		})
	}

	// Perform bulk insert with upsert
	_, err := r.db.NewInsert().
		Model(&users).
		On("CONFLICT (id) DO UPDATE").
		Set("name = EXCLUDED.name").
		Set("display_name = EXCLUDED.display_name").
		Set("description = EXCLUDED.description").
		Set("created_at = EXCLUDED.created_at").
		Set("reason = EXCLUDED.reason").
		Set("groups = EXCLUDED.groups").
		Set("outfits = EXCLUDED.outfits").
		Set("friends = EXCLUDED.friends").
		Set("games = EXCLUDED.games").
		Set("flagged_content = EXCLUDED.flagged_content").
		Set("flagged_groups = EXCLUDED.flagged_groups").
		Set("follower_count = EXCLUDED.follower_count").
		Set("following_count = EXCLUDED.following_count").
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
		r.logger.Error("Failed to save flagged users",
			zap.Error(err),
			zap.Int("userCount", len(flaggedUsers)))
		return err
	}

	r.logger.Debug("Successfully saved flagged users",
		zap.Int("userCount", len(flaggedUsers)))

	return nil
}

// ConfirmUser moves a user from flagged_users to confirmed_users.
// This happens when a moderator confirms that a user is inappropriate.
// The user's groups and friends are tracked to help identify related users.
func (r *UserModel) ConfirmUser(ctx context.Context, user *types.FlaggedUser) error {
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		confirmedUser := &types.ConfirmedUser{
			User:       user.User,
			VerifiedAt: time.Now(),
		}

		_, err := tx.NewInsert().Model(confirmedUser).
			On("CONFLICT (id) DO UPDATE").
			Set("name = EXCLUDED.name").
			Set("display_name = EXCLUDED.display_name").
			Set("description = EXCLUDED.description").
			Set("created_at = EXCLUDED.created_at").
			Set("reason = EXCLUDED.reason").
			Set("groups = EXCLUDED.groups").
			Set("outfits = EXCLUDED.outfits").
			Set("friends = EXCLUDED.friends").
			Set("games = EXCLUDED.games").
			Set("flagged_content = EXCLUDED.flagged_content").
			Set("flagged_groups = EXCLUDED.flagged_groups").
			Set("follower_count = EXCLUDED.follower_count").
			Set("following_count = EXCLUDED.following_count").
			Set("confidence = EXCLUDED.confidence").
			Set("last_scanned = EXCLUDED.last_scanned").
			Set("last_updated = EXCLUDED.last_updated").
			Set("last_viewed = EXCLUDED.last_viewed").
			Set("last_purge_check = EXCLUDED.last_purge_check").
			Set("thumbnail_url = EXCLUDED.thumbnail_url").
			Set("upvotes = EXCLUDED.upvotes").
			Set("downvotes = EXCLUDED.downvotes").
			Set("reputation = EXCLUDED.reputation").
			Set("verified_at = EXCLUDED.verified_at").
			Exec(ctx)
		if err != nil {
			r.logger.Error("Failed to insert or update user in confirmed_users", zap.Error(err), zap.Uint64("userID", user.ID))
			return err
		}

		_, err = tx.NewDelete().Model((*types.FlaggedUser)(nil)).Where("id = ?", user.ID).Exec(ctx)
		if err != nil {
			r.logger.Error("Failed to delete user from flagged_users", zap.Error(err), zap.Uint64("userID", user.ID))
			return err
		}

		return nil
	})
}

// ClearUser moves a user from flagged_users to cleared_users.
// This happens when a moderator determines that a user was incorrectly flagged.
func (r *UserModel) ClearUser(ctx context.Context, user *types.FlaggedUser) error {
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		clearedUser := &types.ClearedUser{
			User:      user.User,
			ClearedAt: time.Now(),
		}

		_, err := tx.NewInsert().Model(clearedUser).
			On("CONFLICT (id) DO UPDATE").
			Set("name = EXCLUDED.name").
			Set("display_name = EXCLUDED.display_name").
			Set("description = EXCLUDED.description").
			Set("created_at = EXCLUDED.created_at").
			Set("reason = EXCLUDED.reason").
			Set("groups = EXCLUDED.groups").
			Set("outfits = EXCLUDED.outfits").
			Set("friends = EXCLUDED.friends").
			Set("games = EXCLUDED.games").
			Set("flagged_content = EXCLUDED.flagged_content").
			Set("flagged_groups = EXCLUDED.flagged_groups").
			Set("follower_count = EXCLUDED.follower_count").
			Set("following_count = EXCLUDED.following_count").
			Set("confidence = EXCLUDED.confidence").
			Set("last_scanned = EXCLUDED.last_scanned").
			Set("last_updated = EXCLUDED.last_updated").
			Set("last_viewed = EXCLUDED.last_viewed").
			Set("last_purge_check = EXCLUDED.last_purge_check").
			Set("thumbnail_url = EXCLUDED.thumbnail_url").
			Set("upvotes = EXCLUDED.upvotes").
			Set("downvotes = EXCLUDED.downvotes").
			Set("reputation = EXCLUDED.reputation").
			Set("cleared_at = EXCLUDED.cleared_at").
			Exec(ctx)
		if err != nil {
			r.logger.Error("Failed to insert or update user in cleared_users", zap.Error(err), zap.Uint64("userID", user.ID))
			return err
		}

		_, err = tx.NewDelete().Model((*types.FlaggedUser)(nil)).Where("id = ?", user.ID).Exec(ctx)
		if err != nil {
			r.logger.Error("Failed to delete user from flagged_users", zap.Error(err), zap.Uint64("userID", user.ID))
			return err
		}

		r.logger.Debug("User cleared and moved to cleared_users", zap.Uint64("userID", user.ID))

		return nil
	})
}

// GetFlaggedUserByIDToReview finds a user in the flagged_users table by their ID
// and updates their last_viewed timestamp.
func (r *UserModel) GetFlaggedUserByIDToReview(ctx context.Context, id uint64) (*types.FlaggedUser, error) {
	var user types.FlaggedUser
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Get the user with row lock
		err := tx.NewSelect().
			Model(&user).
			Where("id = ?", id).
			For("UPDATE SKIP LOCKED").
			Scan(ctx)
		if err != nil {
			r.logger.Error("Failed to get flagged user by ID",
				zap.Error(err),
				zap.Uint64("userID", id))
			return err
		}

		// Update last_viewed
		now := time.Now()
		_, err = tx.NewUpdate().
			Model(&user).
			Set("last_viewed = ?", now).
			Where("id = ?", id).
			Exec(ctx)
		if err != nil {
			r.logger.Error("Failed to update last_viewed",
				zap.Error(err),
				zap.Uint64("userID", id))
			return err
		}
		user.LastViewed = now

		return nil
	})
	if err != nil {
		return nil, err
	}

	r.logger.Debug("Retrieved and updated flagged user by ID",
		zap.Uint64("userID", id),
		zap.Time("lastViewed", user.LastViewed))
	return &user, nil
}

// GetClearedUserByID finds a user in the cleared_users table by their ID.
func (r *UserModel) GetClearedUserByID(ctx context.Context, id uint64) (*types.ClearedUser, error) {
	var user types.ClearedUser
	err := r.db.NewSelect().
		Model(&user).
		Where("id = ?", id).
		Scan(ctx)
	if err != nil {
		r.logger.Error("Failed to get cleared user by ID", zap.Error(err), zap.Uint64("userID", id))
		return nil, err
	}
	r.logger.Debug("Retrieved cleared user by ID", zap.Uint64("userID", id))
	return &user, nil
}

// GetConfirmedUsersCount returns the total number of users in confirmed_users.
func (r *UserModel) GetConfirmedUsersCount(ctx context.Context) (int, error) {
	count, err := r.db.NewSelect().
		Model((*types.ConfirmedUser)(nil)).
		Count(ctx)
	if err != nil {
		r.logger.Error("Failed to get confirmed users count", zap.Error(err))
		return 0, err
	}
	return count, nil
}

// GetFlaggedUsersCount returns the total number of users in flagged_users.
func (r *UserModel) GetFlaggedUsersCount(ctx context.Context) (int, error) {
	count, err := r.db.NewSelect().
		Model((*types.FlaggedUser)(nil)).
		Count(ctx)
	if err != nil {
		r.logger.Error("Failed to get flagged users count", zap.Error(err))
		return 0, err
	}
	return count, nil
}

// GetClearedUsersCount returns the total number of users in cleared_users.
func (r *UserModel) GetClearedUsersCount(ctx context.Context) (int, error) {
	count, err := r.db.NewSelect().
		Model((*types.ClearedUser)(nil)).
		Count(ctx)
	if err != nil {
		r.logger.Error("Failed to get cleared users count", zap.Error(err))
		return 0, err
	}
	return count, nil
}

// CheckExistingUsers finds which users from a list of IDs exist in any user table.
// Returns a map of user IDs to their status (confirmed, flagged, cleared, banned).
func (r *UserModel) CheckExistingUsers(ctx context.Context, userIDs []uint64) (map[uint64]types.UserType, error) {
	var users []struct {
		ID     uint64
		Status types.UserType
	}

	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		err := tx.NewSelect().Model((*types.ConfirmedUser)(nil)).
			Column("id").
			ColumnExpr("? AS status", types.UserTypeConfirmed).
			Where("id IN (?)", bun.In(userIDs)).
			Union(
				tx.NewSelect().Model((*types.FlaggedUser)(nil)).
					Column("id").
					ColumnExpr("? AS status", types.UserTypeFlagged).
					Where("id IN (?)", bun.In(userIDs)),
			).
			Union(
				tx.NewSelect().Model((*types.ClearedUser)(nil)).
					Column("id").
					ColumnExpr("? AS status", types.UserTypeCleared).
					Where("id IN (?)", bun.In(userIDs)),
			).
			Union(
				tx.NewSelect().Model((*types.BannedUser)(nil)).
					Column("id").
					ColumnExpr("? AS status", types.UserTypeBanned).
					Where("id IN (?)", bun.In(userIDs)),
			).
			Scan(ctx, &users)
		return err
	})
	if err != nil {
		r.logger.Error("Failed to check existing users", zap.Error(err))
		return nil, err
	}

	result := make(map[uint64]types.UserType, len(users))
	for _, user := range users {
		result[user.ID] = user.Status
	}

	r.logger.Debug("Checked existing users",
		zap.Int("total", len(userIDs)),
		zap.Int("existing", len(result)))

	return result, nil
}

// GetUsersByIDs retrieves specified user information for a list of user IDs.
// Returns a map of user IDs to user data and a separate map for their types.
func (r *UserModel) GetUsersByIDs(ctx context.Context, userIDs []uint64, fields types.UserFields) (map[uint64]*types.User, map[uint64]types.UserType, error) {
	users := make(map[uint64]*types.User)
	userTypes := make(map[uint64]types.UserType)

	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Query confirmed users
		var confirmedUsers []types.ConfirmedUser
		err := tx.NewSelect().
			Model(&confirmedUsers).
			Column(fields.Columns()...).
			Where("id IN (?)", bun.In(userIDs)).
			Scan(ctx)
		if err != nil {
			return fmt.Errorf("failed to get confirmed users: %w", err)
		}
		for _, user := range confirmedUsers {
			users[user.ID] = &user.User
			userTypes[user.ID] = types.UserTypeConfirmed
		}

		// Query flagged users
		var flaggedUsers []types.FlaggedUser
		err = tx.NewSelect().
			Model(&flaggedUsers).
			Column(fields.Columns()...).
			Where("id IN (?)", bun.In(userIDs)).
			Scan(ctx)
		if err != nil {
			return fmt.Errorf("failed to get flagged users: %w", err)
		}
		for _, user := range flaggedUsers {
			users[user.ID] = &user.User
			userTypes[user.ID] = types.UserTypeFlagged
		}

		// Query cleared users
		var clearedUsers []types.ClearedUser
		err = tx.NewSelect().
			Model(&clearedUsers).
			Column(fields.Columns()...).
			Where("id IN (?)", bun.In(userIDs)).
			Scan(ctx)
		if err != nil {
			return fmt.Errorf("failed to get cleared users: %w", err)
		}
		for _, user := range clearedUsers {
			users[user.ID] = &user.User
			userTypes[user.ID] = types.UserTypeCleared
		}

		// Query banned users
		var bannedUsers []types.BannedUser
		err = tx.NewSelect().
			Model(&bannedUsers).
			Column(fields.Columns()...).
			Where("id IN (?)", bun.In(userIDs)).
			Scan(ctx)
		if err != nil {
			return fmt.Errorf("failed to get banned users: %w", err)
		}
		for _, user := range bannedUsers {
			users[user.ID] = &user.User
			userTypes[user.ID] = types.UserTypeBanned
		}

		return nil
	})
	if err != nil {
		r.logger.Error("Failed to get users by IDs",
			zap.Error(err),
			zap.Uint64s("userIDs", userIDs))
		return nil, nil, err
	}

	r.logger.Debug("Retrieved users by IDs",
		zap.Int("requestedCount", len(userIDs)),
		zap.Int("foundCount", len(users)))

	return users, userTypes, nil
}

// GetUsersToCheck finds users that haven't been checked for banned status recently.
// Returns a batch of user IDs and updates their last_purge_check timestamp.
func (r *UserModel) GetUsersToCheck(ctx context.Context, limit int) ([]uint64, error) {
	var userIDs []uint64
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Get and update confirmed users
		err := tx.NewRaw(`
			WITH updated AS (
				UPDATE confirmed_users
				SET last_purge_check = NOW()
				WHERE id IN (
					SELECT id FROM confirmed_users
					WHERE last_purge_check < NOW() - INTERVAL '1 day'
					ORDER BY last_purge_check ASC
					LIMIT ?
					FOR UPDATE SKIP LOCKED
				)
				RETURNING id
			)
			SELECT * FROM updated
		`, limit/2).Scan(ctx, &userIDs)
		if err != nil {
			r.logger.Error("Failed to get and update confirmed users", zap.Error(err))
			return err
		}

		// Get and update flagged users
		var flaggedIDs []uint64
		err = tx.NewRaw(`
			WITH updated AS (
				UPDATE flagged_users
				SET last_purge_check = NOW()
				WHERE id IN (
					SELECT id FROM flagged_users
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
			r.logger.Error("Failed to get and update flagged users", zap.Error(err))
			return err
		}
		userIDs = append(userIDs, flaggedIDs...)

		return nil
	})

	return userIDs, err
}

// RemoveBannedUsers moves users from confirmed_users and flagged_users to banned_users.
// This happens when users are found to be banned by Roblox.
func (r *UserModel) RemoveBannedUsers(ctx context.Context, userIDs []uint64) error {
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Move confirmed users to banned_users
		var confirmedUsers []types.ConfirmedUser
		err := tx.NewSelect().Model(&confirmedUsers).
			Where("id IN (?)", bun.In(userIDs)).
			Scan(ctx)
		if err != nil {
			r.logger.Error("Failed to select confirmed users for banning", zap.Error(err))
			return err
		}

		for _, user := range confirmedUsers {
			bannedUser := &types.BannedUser{
				User:     user.User,
				PurgedAt: time.Now(),
			}
			_, err = tx.NewInsert().Model(bannedUser).
				On("CONFLICT (id) DO UPDATE").
				Exec(ctx)
			if err != nil {
				r.logger.Error("Failed to insert banned user from confirmed_users", zap.Error(err), zap.Uint64("userID", user.ID))
				return err
			}
		}

		// Move flagged users to banned_users
		var flaggedUsers []types.FlaggedUser
		err = tx.NewSelect().Model(&flaggedUsers).
			Where("id IN (?)", bun.In(userIDs)).
			Scan(ctx)
		if err != nil {
			r.logger.Error("Failed to select flagged users for banning", zap.Error(err))
			return err
		}

		for _, user := range flaggedUsers {
			bannedUser := &types.BannedUser{
				User:     user.User,
				PurgedAt: time.Now(),
			}
			_, err = tx.NewInsert().Model(bannedUser).
				On("CONFLICT (id) DO UPDATE").
				Exec(ctx)
			if err != nil {
				r.logger.Error("Failed to insert banned user from flagged_users", zap.Error(err), zap.Uint64("userID", user.ID))
				return err
			}
		}

		// Remove users from confirmed_users
		_, err = tx.NewDelete().Model((*types.ConfirmedUser)(nil)).
			Where("id IN (?)", bun.In(userIDs)).
			Exec(ctx)
		if err != nil {
			r.logger.Error("Failed to remove banned users from confirmed_users", zap.Error(err))
			return err
		}

		// Remove users from flagged_users
		_, err = tx.NewDelete().Model((*types.FlaggedUser)(nil)).
			Where("id IN (?)", bun.In(userIDs)).
			Exec(ctx)
		if err != nil {
			r.logger.Error("Failed to remove banned users from flagged_users", zap.Error(err))
			return err
		}

		r.logger.Debug("Moved banned users to banned_users", zap.Int("count", len(userIDs)))
		return nil
	})
}

// PurgeOldClearedUsers removes cleared users older than the cutoff date.
// This helps maintain database size by removing users that were cleared long ago.
func (r *UserModel) PurgeOldClearedUsers(ctx context.Context, cutoffDate time.Time) (int, error) {
	result, err := r.db.NewDelete().
		Model((*types.ClearedUser)(nil)).
		Where("cleared_at < ?", cutoffDate).
		Exec(ctx)
	if err != nil {
		r.logger.Error("Failed to purge old cleared users",
			zap.Error(err),
			zap.Time("cutoffDate", cutoffDate))
		return 0, err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		r.logger.Error("Failed to get rows affected", zap.Error(err))
		return 0, err
	}

	r.logger.Debug("Purged old cleared users",
		zap.Int64("rowsAffected", affected),
		zap.Time("cutoffDate", cutoffDate))

	return int(affected), nil
}

// UpdateTrainingVotes updates the upvotes or downvotes count for a user in training mode.
func (r *UserModel) UpdateTrainingVotes(ctx context.Context, userID uint64, isUpvote bool) error {
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Try to update votes in either flagged or confirmed table
		if err := r.updateVotesInTable(ctx, tx, (*types.FlaggedUser)(nil), userID, isUpvote); err == nil {
			return nil
		}
		return r.updateVotesInTable(ctx, tx, (*types.ConfirmedUser)(nil), userID, isUpvote)
	})
	if err != nil {
		r.logger.Error("Failed to update training votes",
			zap.Error(err),
			zap.Uint64("userID", userID),
			zap.String("voteType", map[bool]string{true: "upvote", false: "downvote"}[isUpvote]))
	}
	return err
}

// updateVotesInTable handles updating votes for a specific table type.
func (r *UserModel) updateVotesInTable(ctx context.Context, tx bun.Tx, model interface{}, userID uint64, isUpvote bool) error {
	// Get current vote counts
	var upvotes, downvotes int
	err := tx.NewSelect().
		Model(model).
		Column("upvotes", "downvotes").
		Where("id = ?", userID).
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
		Where("id = ?", userID).
		Exec(ctx)
	return err
}

// GetUserToScan finds the next user to scan from confirmed_users, falling back to flagged_users
// if no confirmed users are available.
func (r *UserModel) GetUserToScan(ctx context.Context) (*types.User, error) {
	var user *types.User
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// First try confirmed users
		var confirmedUser types.ConfirmedUser
		err := tx.NewSelect().Model(&confirmedUser).
			Where("last_scanned < NOW() - INTERVAL '1 day'").
			Order("last_scanned ASC").
			Limit(1).
			For("UPDATE SKIP LOCKED").
			Scan(ctx)
		if err == nil {
			// Update last_scanned
			_, err = tx.NewUpdate().Model(&confirmedUser).
				Set("last_scanned = ?", time.Now()).
				Where("id = ?", confirmedUser.ID).
				Exec(ctx)
			if err != nil {
				r.logger.Error("Failed to update last_scanned for confirmed user", zap.Error(err))
				return err
			}
			user = &confirmedUser.User
			return nil
		}

		// If no confirmed users, try flagged users
		var flaggedUser types.FlaggedUser
		err = tx.NewSelect().Model(&flaggedUser).
			Where("last_scanned < NOW() - INTERVAL '1 day'").
			Order("last_scanned ASC").
			Limit(1).
			For("UPDATE SKIP LOCKED").
			Scan(ctx)
		if err != nil {
			r.logger.Error("Failed to get user to scan", zap.Error(err))
			return err
		}

		// Update last_scanned
		_, err = tx.NewUpdate().Model(&flaggedUser).
			Set("last_scanned = ?", time.Now()).
			Where("id = ?", flaggedUser.ID).
			Exec(ctx)
		if err != nil {
			r.logger.Error("Failed to update last_scanned for flagged user", zap.Error(err))
			return err
		}
		user = &flaggedUser.User
		return nil
	})
	if err != nil {
		return nil, err
	}

	return user, nil
}

// GetUserToReview finds a user to review based on the sort method and target mode.
func (r *UserModel) GetUserToReview(ctx context.Context, sortBy types.SortBy, targetMode types.ReviewTargetMode) (*types.ConfirmedUser, error) {
	var primaryModel, fallbackModel interface{}

	// Set up which models to try first and as fallback based on target mode
	if targetMode == types.FlaggedReviewTarget {
		primaryModel = &types.FlaggedUser{}
		fallbackModel = &types.ConfirmedUser{}
	} else {
		primaryModel = &types.ConfirmedUser{}
		fallbackModel = &types.FlaggedUser{}
	}

	// Try primary target first
	result, err := r.getNextToReview(ctx, primaryModel, sortBy)
	if err == nil {
		if flaggedUser, ok := result.(*types.FlaggedUser); ok {
			return &types.ConfirmedUser{
				User:       flaggedUser.User,
				VerifiedAt: time.Time{}, // Zero time since it's not confirmed yet
			}, nil
		}
		if confirmedUser, ok := result.(*types.ConfirmedUser); ok {
			return confirmedUser, nil
		}
	}

	// Try fallback target
	result, err = r.getNextToReview(ctx, fallbackModel, sortBy)
	if err == nil {
		if flaggedUser, ok := result.(*types.FlaggedUser); ok {
			return &types.ConfirmedUser{
				User:       flaggedUser.User,
				VerifiedAt: time.Time{}, // Zero time since it's not confirmed yet
			}, nil
		}
		if confirmedUser, ok := result.(*types.ConfirmedUser); ok {
			return confirmedUser, nil
		}
	}

	return nil, types.ErrNoUsersToReview
}

// getNextToReview handles the common logic for getting the next item to review.
func (r *UserModel) getNextToReview(ctx context.Context, model interface{}, sortBy types.SortBy) (interface{}, error) {
	var result interface{}
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		query := tx.NewSelect().
			Model(model).
			Where("last_viewed < NOW() - INTERVAL '10 minutes'")

		// Apply sort order
		switch sortBy {
		case types.SortByConfidence:
			query.Order("confidence DESC")
		case types.SortByLastUpdated:
			query.Order("last_updated ASC")
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
		case *types.FlaggedUser:
			m.LastViewed = now
			id = m.ID
			result = m
		case *types.ConfirmedUser:
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

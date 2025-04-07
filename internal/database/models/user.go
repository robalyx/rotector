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

// UserModel handles database operations for user records.
type UserModel struct {
	db     *bun.DB
	logger *zap.Logger
}

// NewUser creates a UserModel.
func NewUser(db *bun.DB, logger *zap.Logger) *UserModel {
	return &UserModel{
		db:     db,
		logger: logger.Named("db_user"),
	}
}

// SaveUsersByStatus saves users that have already been grouped by status.
//
// Deprecated: Use Service().User().SaveUsers() instead.
func (r *UserModel) SaveUsersByStatus(
	ctx context.Context,
	flaggedUsers []*types.FlaggedUser,
	confirmedUsers []*types.ConfirmedUser,
	clearedUsers []*types.ClearedUser,
) error {
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Helper function to update a table
		updateTable := func(users any, status enum.UserType) error {
			_, err := tx.NewInsert().
				Model(users).
				On("CONFLICT (id) DO UPDATE").
				Set("uuid = EXCLUDED.uuid").
				Set("name = EXCLUDED.name").
				Set("display_name = EXCLUDED.display_name").
				Set("description = EXCLUDED.description").
				Set("created_at = EXCLUDED.created_at").
				Set("reasons = EXCLUDED.reasons").
				Set("groups = EXCLUDED.groups").
				Set("outfits = EXCLUDED.outfits").
				Set("friends = EXCLUDED.friends").
				Set("games = EXCLUDED.games").
				Set("inventory = EXCLUDED.inventory").
				Set("confidence = EXCLUDED.confidence").
				Set("has_socials = EXCLUDED.has_socials").
				Set("last_scanned = EXCLUDED.last_scanned").
				Set("last_updated = EXCLUDED.last_updated").
				Set("last_viewed = EXCLUDED.last_viewed").
				Set("last_ban_check = EXCLUDED.last_ban_check").
				Set("is_banned = EXCLUDED.is_banned").
				Set("is_deleted = EXCLUDED.is_deleted").
				Set("thumbnail_url = EXCLUDED.thumbnail_url").
				Set("last_thumbnail_update = EXCLUDED.last_thumbnail_update").
				Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to update %s users: %w", status, err)
			}
			return nil
		}

		// Update each table with its corresponding slice
		if len(flaggedUsers) > 0 {
			if err := updateTable(&flaggedUsers, enum.UserTypeFlagged); err != nil {
				return err
			}
		}

		if len(confirmedUsers) > 0 {
			if err := updateTable(&confirmedUsers, enum.UserTypeConfirmed); err != nil {
				return err
			}
		}

		if len(clearedUsers) > 0 {
			if err := updateTable(&clearedUsers, enum.UserTypeCleared); err != nil {
				return err
			}
		}

		return nil
	})
}

// ConfirmUser moves a user from other user tables to confirmed_users.
//
// Deprecated: Use Service().User().ConfirmUser() instead.
func (r *UserModel) ConfirmUser(ctx context.Context, user *types.ReviewUser) error {
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		confirmedUser := &types.ConfirmedUser{
			User:       user.User,
			VerifiedAt: time.Now(),
		}

		// Try to move user to confirmed_users table
		result, err := tx.NewInsert().Model(confirmedUser).
			On("CONFLICT (id) DO NOTHING").
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to insert user in confirmed_users: %w (userID=%d)", err, user.ID)
		}

		affected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("failed to get rows affected: %w", err)
		}
		if affected == 0 {
			return nil // Skip if there was a conflict
		}

		// Delete from other tables
		_, err = tx.NewDelete().Model((*types.FlaggedUser)(nil)).Where("id = ?", user.ID).Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to delete user from flagged_users: %w (userID=%d)", err, user.ID)
		}

		_, err = tx.NewDelete().Model((*types.ClearedUser)(nil)).Where("id = ?", user.ID).Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to delete user from cleared_users: %w (userID=%d)", err, user.ID)
		}

		return nil
	})
}

// ClearUser moves a user from other user tables to cleared_users.
//
// Deprecated: Use Service().User().ClearUser() instead.
func (r *UserModel) ClearUser(ctx context.Context, user *types.ReviewUser) error {
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		clearedUser := &types.ClearedUser{
			User:      user.User,
			ClearedAt: time.Now(),
		}

		// Try to move user to cleared_users table
		result, err := tx.NewInsert().Model(clearedUser).
			On("CONFLICT (id) DO NOTHING").
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to insert user in cleared_users: %w", err)
		}

		affected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("failed to get rows affected: %w", err)
		}
		if affected == 0 {
			return nil // Skip if there was a conflict
		}

		// Delete from other tables
		_, err = tx.NewDelete().Model((*types.FlaggedUser)(nil)).Where("id = ?", user.ID).Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to delete user from flagged_users: %w", err)
		}

		_, err = tx.NewDelete().Model((*types.ConfirmedUser)(nil)).Where("id = ?", user.ID).Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to delete user from confirmed_users: %w", err)
		}

		return nil
	})
}

// GetConfirmedUsersCount returns the total number of users in confirmed_users.
func (r *UserModel) GetConfirmedUsersCount(ctx context.Context) (int, error) {
	count, err := r.db.NewSelect().
		Model((*types.ConfirmedUser)(nil)).
		Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get confirmed users count: %w", err)
	}
	return count, nil
}

// GetFlaggedUsersCount returns the total number of users in flagged_users.
func (r *UserModel) GetFlaggedUsersCount(ctx context.Context) (int, error) {
	count, err := r.db.NewSelect().
		Model((*types.FlaggedUser)(nil)).
		Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get flagged users count: %w", err)
	}
	return count, nil
}

// GetRecentlyProcessedUsers checks which users exist in any table and have been updated within the past 7 days.
// For confirmed users, we consider them as processed regardless of when they were last updated.
func (r *UserModel) GetRecentlyProcessedUsers(ctx context.Context, userIDs []uint64) (map[uint64]enum.UserType, error) {
	var users []struct {
		ID     uint64
		Status enum.UserType
	}

	err := r.db.NewSelect().Model((*types.ConfirmedUser)(nil)).
		Column("id").
		ColumnExpr("? AS status", enum.UserTypeConfirmed).
		Where("id IN (?)", bun.In(userIDs)).
		Union(
			r.db.NewSelect().Model((*types.FlaggedUser)(nil)).
				Column("id").
				ColumnExpr("? AS status", enum.UserTypeFlagged).
				Where("id IN (?)", bun.In(userIDs)).
				Where("last_updated > NOW() - INTERVAL '7 days'"),
		).
		Union(
			r.db.NewSelect().Model((*types.ClearedUser)(nil)).
				Column("id").
				ColumnExpr("? AS status", enum.UserTypeCleared).
				Where("id IN (?)", bun.In(userIDs)).
				Where("last_updated > NOW() - INTERVAL '7 days'"),
		).
		Scan(ctx, &users)
	if err != nil {
		return nil, fmt.Errorf("failed to check recently processed users: %w", err)
	}

	result := make(map[uint64]enum.UserType, len(users))
	for _, user := range users {
		result[user.ID] = user.Status
	}

	r.logger.Debug("Checked recently processed users",
		zap.Int("total", len(userIDs)),
		zap.Int("existing", len(result)))

	return result, nil
}

// GetUserByID retrieves a user by either their numeric ID or UUID.
//
// Deprecated: Use Service().User().GetUserByID() instead.
func (r *UserModel) GetUserByID(ctx context.Context, userID string, fields types.UserField) (*types.ReviewUser, error) {
	var result types.ReviewUser
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Try each model in order until we find a user
		models := []any{
			&types.FlaggedUser{},
			&types.ConfirmedUser{},
			&types.ClearedUser{},
		}

		for _, model := range models {
			query := tx.NewSelect().
				Model(model).
				Column(fields.Columns()...).
				For("UPDATE")

			// Check if input is numeric (ID) or string (UUID)
			if id, err := strconv.ParseUint(userID, 10, 64); err == nil {
				query.Where("id = ?", id)
			} else {
				// Parse UUID string
				uid, err := uuid.Parse(userID)
				if err != nil {
					return types.ErrInvalidUserID
				}
				query.Where("uuid = ?", uid)
			}

			err := query.Scan(ctx)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					continue
				}
				return fmt.Errorf("failed to get user: %w", err)
			}

			// Set result based on model type
			switch m := model.(type) {
			case *types.FlaggedUser:
				result.User = m.User
				result.Status = enum.UserTypeFlagged
			case *types.ConfirmedUser:
				result.User = m.User
				result.VerifiedAt = m.VerifiedAt
				result.Status = enum.UserTypeConfirmed
			case *types.ClearedUser:
				result.User = m.User
				result.ClearedAt = m.ClearedAt
				result.Status = enum.UserTypeCleared
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

		return types.ErrUserNotFound
	})
	if err != nil {
		return nil, err
	}

	return &result, nil
}

// GetUsersByIDs retrieves specified user information for a list of user IDs.
// Returns a map of user IDs to review users.
func (r *UserModel) GetUsersByIDs(
	ctx context.Context, userIDs []uint64, fields types.UserField,
) (map[uint64]*types.ReviewUser, error) {
	users := make(map[uint64]*types.ReviewUser)
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Build query with selected fields
		columns := fields.Columns()

		// Query confirmed users
		var confirmedUsers []types.ConfirmedUser
		err := tx.NewSelect().
			Model(&confirmedUsers).
			Column(columns...).
			Where("id IN (?)", bun.In(userIDs)).
			Scan(ctx)
		if err != nil {
			return fmt.Errorf("failed to get confirmed users: %w", err)
		}
		for _, user := range confirmedUsers {
			users[user.ID] = &types.ReviewUser{
				User:       user.User,
				VerifiedAt: user.VerifiedAt,
				Status:     enum.UserTypeConfirmed,
			}
		}

		// Query flagged users
		var flaggedUsers []types.FlaggedUser
		err = tx.NewSelect().
			Model(&flaggedUsers).
			Column(columns...).
			Where("id IN (?)", bun.In(userIDs)).
			Scan(ctx)
		if err != nil {
			return fmt.Errorf("failed to get flagged users: %w", err)
		}
		for _, user := range flaggedUsers {
			users[user.ID] = &types.ReviewUser{
				User:   user.User,
				Status: enum.UserTypeFlagged,
			}
		}

		// Query cleared users
		var clearedUsers []types.ClearedUser
		err = tx.NewSelect().
			Model(&clearedUsers).
			Column(columns...).
			Where("id IN (?)", bun.In(userIDs)).
			Scan(ctx)
		if err != nil {
			return fmt.Errorf("failed to get cleared users: %w", err)
		}
		for _, user := range clearedUsers {
			users[user.ID] = &types.ReviewUser{
				User:      user.User,
				ClearedAt: user.ClearedAt,
				Status:    enum.UserTypeCleared,
			}
		}

		r.logger.Debug("Retrieved users by IDs",
			zap.Int("requestedCount", len(userIDs)),
			zap.Int("foundCount", len(users)))

		return nil
	})
	if err != nil {
		return nil, err
	}

	return users, nil
}

// GetFlaggedAndConfirmedUsers retrieves all flagged and confirmed users.
func (r *UserModel) GetFlaggedAndConfirmedUsers(ctx context.Context) ([]*types.ReviewUser, error) {
	var users []*types.ReviewUser
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Get flagged users
		var flaggedUsers []types.FlaggedUser
		err := tx.NewSelect().
			Model(&flaggedUsers).
			Column("id", "reasons", "confidence").
			Scan(ctx)
		if err != nil {
			return fmt.Errorf("failed to get flagged users: %w", err)
		}
		for _, user := range flaggedUsers {
			users = append(users, &types.ReviewUser{
				User:   user.User,
				Status: enum.UserTypeFlagged,
			})
		}

		// Get confirmed users
		var confirmedUsers []types.ConfirmedUser
		err = tx.NewSelect().
			Model(&confirmedUsers).
			Column("id", "reasons", "confidence").
			Scan(ctx)
		if err != nil {
			return fmt.Errorf("failed to get confirmed users: %w", err)
		}
		for _, user := range confirmedUsers {
			users = append(users, &types.ReviewUser{
				User:   user.User,
				Status: enum.UserTypeConfirmed,
			})
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return users, nil
}

// GetUsersToCheck finds users that haven't been checked for banned status recently.
func (r *UserModel) GetUsersToCheck(
	ctx context.Context, limit int,
) (userIDs []uint64, bannedIDs []uint64, err error) {
	err = r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Get and update confirmed users
		var confirmedUsers []types.ConfirmedUser
		err := tx.NewSelect().
			Model(&confirmedUsers).
			Column("id", "is_banned").
			Where("last_ban_check < NOW() - INTERVAL '1 day'").
			OrderExpr("last_ban_check ASC").
			Limit(limit / 2).
			For("UPDATE SKIP LOCKED").
			Scan(ctx)
		if err != nil {
			return fmt.Errorf("failed to get confirmed users: %w", err)
		}

		if len(confirmedUsers) > 0 {
			userIDs = make([]uint64, 0, len(confirmedUsers))
			for _, user := range confirmedUsers {
				userIDs = append(userIDs, user.ID)
				if user.IsBanned {
					bannedIDs = append(bannedIDs, user.ID)
				}
			}

			// Update last_ban_check
			_, err = tx.NewUpdate().
				Model(&confirmedUsers).
				Set("last_ban_check = NOW()").
				Where("id IN (?)", bun.In(userIDs)).
				Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to update confirmed users: %w", err)
			}
		}

		// Calculate remaining limit for flagged users
		remainingLimit := limit - len(confirmedUsers)
		if remainingLimit <= 0 {
			return nil
		}

		// Get and update flagged users
		var flaggedUsers []types.FlaggedUser
		err = tx.NewSelect().
			Model(&flaggedUsers).
			Column("id", "is_banned").
			Where("last_ban_check < NOW() - INTERVAL '1 day'").
			OrderExpr("last_ban_check ASC").
			Limit(remainingLimit).
			For("UPDATE SKIP LOCKED").
			Scan(ctx)
		if err != nil {
			return fmt.Errorf("failed to get flagged users: %w", err)
		}

		if len(flaggedUsers) > 0 {
			flaggedIDs := make([]uint64, 0, len(flaggedUsers))
			for _, user := range flaggedUsers {
				flaggedIDs = append(flaggedIDs, user.ID)
				if user.IsBanned {
					bannedIDs = append(bannedIDs, user.ID)
				}
			}

			// Update last_ban_check
			_, err = tx.NewUpdate().
				Model(&flaggedUsers).
				Set("last_ban_check = NOW()").
				Where("id IN (?)", bun.In(flaggedIDs)).
				Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to update flagged users: %w", err)
			}

			userIDs = append(userIDs, flaggedIDs...)
		}

		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	return userIDs, bannedIDs, nil
}

// MarkUsersBanStatus updates the banned status of users in their respective tables.
func (r *UserModel) MarkUsersBanStatus(ctx context.Context, userIDs []uint64, isBanned bool) error {
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Update confirmed users
		_, err := tx.NewUpdate().
			Model((*types.ConfirmedUser)(nil)).
			Set("is_banned = ?", isBanned).
			Where("id IN (?)", bun.In(userIDs)).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to mark confirmed users ban status: %w", err)
		}

		// Update flagged users
		_, err = tx.NewUpdate().
			Model((*types.FlaggedUser)(nil)).
			Set("is_banned = ?", isBanned).
			Where("id IN (?)", bun.In(userIDs)).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to mark flagged users ban status: %w", err)
		}

		r.logger.Debug("Marked users ban status",
			zap.Int("count", len(userIDs)),
			zap.Bool("isBanned", isBanned))
		return nil
	})
}

// GetBannedCount returns the total number of banned users across all tables.
func (r *UserModel) GetBannedCount(ctx context.Context) (int, error) {
	count, err := r.db.NewSelect().
		TableExpr("(?) AS banned_users", r.db.NewSelect().
			Model((*types.ConfirmedUser)(nil)).
			Column("id").
			Where("is_banned = true").
			UnionAll(
				r.db.NewSelect().
					Model((*types.FlaggedUser)(nil)).
					Column("id").
					Where("is_banned = true"),
			),
		).
		Count(ctx)
	if err != nil {
		return 0, err
	}
	return count, nil
}

// GetUserCounts returns counts for all user statuses.
func (r *UserModel) GetUserCounts(ctx context.Context) (*types.UserCounts, error) {
	var counts types.UserCounts
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		confirmedCount, err := tx.NewSelect().Model((*types.ConfirmedUser)(nil)).Count(ctx)
		if err != nil {
			return fmt.Errorf("failed to get confirmed users count: %w", err)
		}
		counts.Confirmed = confirmedCount

		flaggedCount, err := tx.NewSelect().Model((*types.FlaggedUser)(nil)).Count(ctx)
		if err != nil {
			return fmt.Errorf("failed to get flagged users count: %w", err)
		}
		counts.Flagged = flaggedCount

		clearedCount, err := tx.NewSelect().Model((*types.ClearedUser)(nil)).Count(ctx)
		if err != nil {
			return fmt.Errorf("failed to get cleared users count: %w", err)
		}
		counts.Cleared = clearedCount

		bannedCount, err := r.GetBannedCount(ctx)
		if err != nil {
			return err
		}
		counts.Banned = bannedCount

		return nil
	})
	if err != nil {
		return nil, err
	}

	return &counts, nil
}

// PurgeOldClearedUsers removes cleared users older than the cutoff date.
// This helps maintain database size by removing users that were cleared long ago.
func (r *UserModel) PurgeOldClearedUsers(ctx context.Context, cutoffDate time.Time) (int, error) {
	result, err := r.db.NewDelete().
		Model((*types.ClearedUser)(nil)).
		Where("cleared_at < ?", cutoffDate).
		Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to purge old cleared users: %w (cutoffDate=%s)", err, cutoffDate.Format(time.RFC3339))
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w (cutoffDate=%s)", err, cutoffDate.Format(time.RFC3339))
	}

	r.logger.Debug("Purged old cleared users",
		zap.Int64("rowsAffected", affected),
		zap.Time("cutoffDate", cutoffDate))

	return int(affected), nil
}

// GetUsersForThumbnailUpdate retrieves users that need thumbnail updates.
func (r *UserModel) GetUsersForThumbnailUpdate(ctx context.Context, limit int) (map[uint64]*types.User, error) {
	users := make(map[uint64]*types.User)
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Query users from each table that need thumbnail updates
		for _, model := range []any{
			(*types.FlaggedUser)(nil),
			(*types.ConfirmedUser)(nil),
			(*types.ClearedUser)(nil),
		} {
			var reviewUsers []types.ReviewUser
			err := tx.NewSelect().
				Model(model).
				Where("last_thumbnail_update < NOW() - INTERVAL '7 days'").
				Where("is_deleted = false").
				OrderExpr("last_thumbnail_update ASC").
				Limit(limit).
				Scan(ctx, &reviewUsers)

			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("failed to query users for thumbnail update: %w", err)
			}

			for _, review := range reviewUsers {
				users[review.ID] = &review.User
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return users, nil
}

// DeleteUser removes a user and all associated data from the database.
func (r *UserModel) DeleteUser(ctx context.Context, userID uint64) (bool, error) {
	var totalAffected int64
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Delete from flagged_users
		result, err := tx.NewDelete().
			Model((*types.FlaggedUser)(nil)).
			Where("id = ?", userID).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to delete from flagged_users: %w", err)
		}
		affected, _ := result.RowsAffected()
		totalAffected += affected

		// Delete from confirmed_users
		result, err = tx.NewDelete().
			Model((*types.ConfirmedUser)(nil)).
			Where("id = ?", userID).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to delete from confirmed_users: %w", err)
		}
		affected, _ = result.RowsAffected()
		totalAffected += affected

		// Delete from cleared_users
		result, err = tx.NewDelete().
			Model((*types.ClearedUser)(nil)).
			Where("id = ?", userID).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to delete from cleared_users: %w", err)
		}
		affected, _ = result.RowsAffected()
		totalAffected += affected

		return nil
	})
	if err != nil {
		return false, err
	}

	return totalAffected > 0, nil
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
			OrderExpr("last_scanned ASC, confidence DESC").
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
				return fmt.Errorf(
					"failed to update last_scanned for confirmed user: %w (userID=%d)",
					err, confirmedUser.ID,
				)
			}
			user = &confirmedUser.User
			return nil
		}

		if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("failed to query confirmed users: %w", err)
		}

		// If no confirmed users, try flagged users
		var flaggedUser types.FlaggedUser
		err = tx.NewSelect().Model(&flaggedUser).
			Where("last_scanned < NOW() - INTERVAL '1 day'").
			Where("confidence >= 0.8").
			OrderExpr("last_scanned ASC, confidence DESC").
			Limit(1).
			For("UPDATE SKIP LOCKED").
			Scan(ctx)

		if err == nil {
			// Update last_scanned
			_, err = tx.NewUpdate().Model(&flaggedUser).
				Set("last_scanned = ?", time.Now()).
				Where("id = ?", flaggedUser.ID).
				Exec(ctx)
			if err != nil {
				return fmt.Errorf(
					"failed to update last_scanned for flagged user: %w (userID=%d)",
					err, flaggedUser.ID,
				)
			}
			user = &flaggedUser.User
			return nil
		}

		if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("failed to query flagged users: %w", err)
		}

		// No users found to scan
		return fmt.Errorf("no users available to scan: %w", err)
	})
	if err != nil {
		return nil, err
	}

	return user, nil
}

// GetNextToReview handles the database operations for getting the next user to review.
//
// Deprecated: Use Service().User().GetUserToReview() instead.
func (r *UserModel) GetNextToReview(
	ctx context.Context, model any, sortBy enum.ReviewSortBy, recentIDs []uint64,
) (*types.ReviewUser, error) {
	var result types.ReviewUser
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
			subq.OrderExpr("confidence DESC, last_viewed ASC")
		case enum.ReviewSortByLastUpdated:
			subq.OrderExpr("last_updated ASC, last_viewed ASC")
		case enum.ReviewSortByReputation:
			subq.Join("LEFT JOIN user_reputations ON user_reputations.id = ?TableAlias.id").
				OrderExpr("COALESCE(user_reputations.score, 0) ASC, last_viewed ASC")
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
		case *types.FlaggedUser:
			result.User = m.User
			result.Status = enum.UserTypeFlagged
		case *types.ConfirmedUser:
			result.User = m.User
			result.VerifiedAt = m.VerifiedAt
			result.Status = enum.UserTypeConfirmed
		case *types.ClearedUser:
			result.User = m.User
			result.ClearedAt = m.ClearedAt
			result.Status = enum.UserTypeCleared
		default:
			return fmt.Errorf("%w: %T", types.ErrUnsupportedModel, model)
		}

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

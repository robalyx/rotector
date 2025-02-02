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

// UserModel handles database operations for user records.
type UserModel struct {
	db         *bun.DB
	tracking   *TrackingModel
	activity   *ActivityModel
	reputation *ReputationModel
	votes      *VoteModel
	logger     *zap.Logger
}

// NewUser creates a UserModel with references to the tracking system.
func NewUser(
	db *bun.DB,
	tracking *TrackingModel,
	activity *ActivityModel,
	reputation *ReputationModel,
	votes *VoteModel,
	logger *zap.Logger,
) *UserModel {
	return &UserModel{
		db:         db,
		tracking:   tracking,
		activity:   activity,
		reputation: reputation,
		votes:      votes,
		logger:     logger,
	}
}

// SaveUsers updates or inserts users into their appropriate tables based on their current status.
func (r *UserModel) SaveUsers(ctx context.Context, users map[uint64]*types.User) error {
	// Get list of user IDs to check
	userIDs := make([]uint64, 0, len(users))
	for id := range users {
		userIDs = append(userIDs, id)
	}

	// Get existing users with all their data
	existingUsers, err := r.GetUsersByIDs(ctx, userIDs, types.UserFieldBasic|types.UserFieldTimestamps)
	if err != nil {
		return fmt.Errorf("failed to get existing users: %w", err)
	}

	// Initialize slices for each table
	flaggedUsers := make([]*types.FlaggedUser, 0)
	confirmedUsers := make([]*types.ConfirmedUser, 0)
	clearedUsers := make([]*types.ClearedUser, 0)
	counts := make(map[enum.UserType]int)

	// Group users by their target tables
	for id, user := range users {
		// Generate UUID for new users
		if user.UUID == uuid.Nil {
			user.UUID = uuid.New()
		}

		// Get existing user data if available
		var status enum.UserType
		existingUser := existingUsers[id]
		if existingUser.Status != enum.UserTypeUnflagged {
			status = existingUser.Status
		} else {
			// Default to flagged_users for new users
			status = enum.UserTypeFlagged
		}

		switch status {
		case enum.UserTypeConfirmed:
			confirmedUsers = append(confirmedUsers, &types.ConfirmedUser{
				User:       *user,
				VerifiedAt: existingUser.VerifiedAt,
			})
		case enum.UserTypeFlagged:
			flaggedUsers = append(flaggedUsers, &types.FlaggedUser{
				User: *user,
			})
		case enum.UserTypeCleared:
			clearedUsers = append(clearedUsers, &types.ClearedUser{
				User:      *user,
				ClearedAt: existingUser.ClearedAt,
			})
		case enum.UserTypeUnflagged:
			continue
		}
		counts[status]++
	}

	// Update each table
	err = r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Helper function to update a table
		updateTable := func(users interface{}, status enum.UserType) error {
			if counts[status] == 0 {
				return nil
			}

			_, err := tx.NewInsert().
				Model(users).
				On("CONFLICT (id) DO UPDATE").
				Set("uuid = EXCLUDED.uuid").
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
				Set("follower_count = EXCLUDED.follower_count").
				Set("following_count = EXCLUDED.following_count").
				Set("confidence = EXCLUDED.confidence").
				Set("last_scanned = EXCLUDED.last_scanned").
				Set("last_updated = EXCLUDED.last_updated").
				Set("last_viewed = EXCLUDED.last_viewed").
				Set("last_ban_check = EXCLUDED.last_ban_check").
				Set("is_banned = EXCLUDED.is_banned").
				Set("thumbnail_url = EXCLUDED.thumbnail_url").
				Set("last_thumbnail_update = EXCLUDED.last_thumbnail_update").
				Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to update %s users: %w", status, err)
			}
			return nil
		}

		// Update each table with its corresponding slice
		if err := updateTable(&flaggedUsers, enum.UserTypeFlagged); err != nil {
			return err
		}
		if err := updateTable(&confirmedUsers, enum.UserTypeConfirmed); err != nil {
			return err
		}
		if err := updateTable(&clearedUsers, enum.UserTypeCleared); err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to save users: %w", err)
	}

	r.logger.Debug("Successfully saved users",
		zap.Int("totalUsers", len(users)),
		zap.Int("flaggedUsers", counts[enum.UserTypeFlagged]),
		zap.Int("confirmedUsers", counts[enum.UserTypeConfirmed]),
		zap.Int("clearedUsers", counts[enum.UserTypeCleared]))

	return nil
}

// ConfirmUser moves a user from other user tables to confirmed_users.
func (r *UserModel) ConfirmUser(ctx context.Context, user *types.ReviewUser) error {
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
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
	if err != nil {
		return err
	}

	// Verify votes for the user
	if err := r.votes.VerifyVotes(ctx, user.ID, true, enum.VoteTypeUser); err != nil {
		r.logger.Error("Failed to verify votes", zap.Error(err))
		return err
	}

	return nil
}

// ClearUser moves a user from other user tables to cleared_users.
func (r *UserModel) ClearUser(ctx context.Context, user *types.ReviewUser) error {
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
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
	if err != nil {
		return err
	}

	// Verify votes for the user
	if err := r.votes.VerifyVotes(ctx, user.ID, false, enum.VoteTypeUser); err != nil {
		r.logger.Error("Failed to verify votes", zap.Error(err))
		return err
	}

	return nil
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
// Returns a map of user IDs to their current status.
func (r *UserModel) GetRecentlyProcessedUsers(ctx context.Context, userIDs []uint64) (map[uint64]enum.UserType, error) {
	var users []struct {
		ID     uint64
		Status enum.UserType
	}

	err := r.db.NewSelect().Model((*types.ConfirmedUser)(nil)).
		Column("id").
		ColumnExpr("? AS status", enum.UserTypeConfirmed).
		Where("id IN (?)", bun.In(userIDs)).
		Where("last_updated > NOW() - INTERVAL '7 days'").
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
func (r *UserModel) GetUserByID(ctx context.Context, userID string, fields types.UserField) (*types.ReviewUser, error) {
	var result types.ReviewUser
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Try each model in order until we find a user
		models := []interface{}{
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
			if err == nil {
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

				// Get reputation
				reputation, err := r.reputation.GetUserReputation(ctx, result.ID)
				if err != nil {
					return fmt.Errorf("failed to get user reputation: %w", err)
				}
				result.Reputation = reputation

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

		return types.ErrUserNotFound
	})
	if err != nil {
		return nil, err
	}

	return &result, nil
}

// GetUsersByIDs retrieves specified user information for a list of user IDs.
// Returns a map of user IDs to review users.
func (r *UserModel) GetUsersByIDs(ctx context.Context, userIDs []uint64, fields types.UserField) (map[uint64]*types.ReviewUser, error) {
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

		// Mark remaining IDs as unflagged
		for _, id := range userIDs {
			if _, ok := users[id]; !ok {
				users[id] = &types.ReviewUser{
					User:   types.User{ID: id},
					Status: enum.UserTypeUnflagged,
				}
			}
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get users by IDs: %w (userCount=%d)", err, len(userIDs))
	}

	r.logger.Debug("Retrieved users by IDs",
		zap.Int("requestedCount", len(userIDs)),
		zap.Int("foundCount", len(users)))

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
			Column("id", "reason").
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
			Column("id", "reason").
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

	return users, err
}

// GetUsersToCheck finds users that haven't been checked for banned status recently.
// Returns two slices: users to check, and currently banned users among those to check.
func (r *UserModel) GetUsersToCheck(ctx context.Context, limit int) ([]uint64, []uint64, error) {
	var userIDs []uint64
	var bannedIDs []uint64
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
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

	return userIDs, bannedIDs, err
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

// GetBannedCount returns the total number of banned users across all tables
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
		return 0, fmt.Errorf("failed to get banned users count: %w", err)
	}

	return count, nil
}

// GetUserCounts returns counts for all user statuses
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
		return nil, fmt.Errorf("failed to get user counts: %w", err)
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
		for _, model := range []interface{}{
			(*types.FlaggedUser)(nil),
			(*types.ConfirmedUser)(nil),
			(*types.ClearedUser)(nil),
		} {
			var reviewUsers []types.ReviewUser
			err := tx.NewSelect().
				Model(model).
				Where("last_thumbnail_update < NOW() - INTERVAL '7 days'").
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

	return users, err
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

	return totalAffected > 0, err
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
			Order("last_scanned ASC").
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

// GetUserToReview finds a user to review based on the sort method and target mode.
func (r *UserModel) GetUserToReview(ctx context.Context, sortBy enum.ReviewSortBy, targetMode enum.ReviewTargetMode, reviewerID uint64) (*types.ReviewUser, error) {
	// Get recently reviewed user IDs
	recentIDs, err := r.activity.GetRecentlyReviewedIDs(ctx, reviewerID, false, 100)
	if err != nil {
		r.logger.Error("Failed to get recently reviewed user IDs", zap.Error(err))
		// Continue without filtering if there's an error
		recentIDs = []uint64{}
	}

	// Define models in priority order based on target mode
	var models []interface{}
	switch targetMode {
	case enum.ReviewTargetModeFlagged:
		models = []interface{}{
			&types.FlaggedUser{},   // Primary target
			&types.ConfirmedUser{}, // First fallback
			&types.ClearedUser{},   // Second fallback
		}
	case enum.ReviewTargetModeConfirmed:
		models = []interface{}{
			&types.ConfirmedUser{}, // Primary target
			&types.FlaggedUser{},   // First fallback
			&types.ClearedUser{},   // Second fallback
		}
	case enum.ReviewTargetModeCleared:
		models = []interface{}{
			&types.ClearedUser{},   // Primary target
			&types.FlaggedUser{},   // First fallback
			&types.ConfirmedUser{}, // Second fallback
		}
	}

	// Try each model in order until we find a user
	for _, model := range models {
		result, err := r.getNextToReview(ctx, model, sortBy, recentIDs)
		if err == nil {
			return result, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}

	return nil, types.ErrNoUsersToReview
}

// getNextToReview handles the common logic for getting the next item to review.
func (r *UserModel) getNextToReview(ctx context.Context, model interface{}, sortBy enum.ReviewSortBy, recentIDs []uint64) (*types.ReviewUser, error) {
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
			subq.Order("confidence DESC")
		case enum.ReviewSortByLastUpdated:
			subq.Order("last_updated ASC")
		case enum.ReviewSortByReputation:
			subq.Join("LEFT JOIN user_reputations ON user_reputations.id = ?TableAlias.id").
				OrderExpr("COALESCE(user_reputations.score, 0) ASC")
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

		// Get reputation
		reputation, err := r.reputation.GetUserReputation(ctx, result.ID)
		if err != nil {
			return fmt.Errorf("failed to get user reputation: %w", err)
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

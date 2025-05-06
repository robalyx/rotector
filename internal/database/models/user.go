package models

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	apiTypes "github.com/jaxron/roapi.go/pkg/api/types"
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

// SaveUsers saves users to the database.
//
// Deprecated: Use Service().User().SaveUsers() instead.
func (r *UserModel) SaveUsers(ctx context.Context, tx bun.Tx, users []*types.ReviewUser) error {
	if len(users) == 0 {
		return nil
	}

	// Convert review user to database user
	dbUsers := make([]*types.User, len(users))
	for i, user := range users {
		dbUsers[i] = user.User
	}

	// Update users table with core data
	_, err := tx.NewInsert().
		Model(&dbUsers).
		On("CONFLICT (id) DO UPDATE").
		Set("uuid = EXCLUDED.uuid").
		Set("name = EXCLUDED.name").
		Set("display_name = EXCLUDED.display_name").
		Set("description = EXCLUDED.description").
		Set("created_at = EXCLUDED.created_at").
		Set("status = EXCLUDED.status").
		Set("reasons = EXCLUDED.reasons").
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
		return fmt.Errorf("failed to upsert users: %w", err)
	}

	return nil
}

// ConfirmUser moves a user to confirmed status and creates a verification record.
//
// Deprecated: Use Service().User().ConfirmUser() instead.
func (r *UserModel) ConfirmUser(ctx context.Context, user *types.ReviewUser) error {
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Update user status
		_, err := tx.NewUpdate().
			Model(user.User).
			Set("status = ?", enum.UserTypeConfirmed).
			Where("id = ?", user.ID).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to update user status: %w", err)
		}

		// Create verification record
		verification := &types.UserVerification{
			UserID:     user.ID,
			ReviewerID: user.ReviewerID,
			VerifiedAt: time.Now(),
		}
		_, err = tx.NewInsert().
			Model(verification).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to create verification record: %w", err)
		}

		return nil
	})
}

// ClearUser moves a user to cleared status and creates a clearance record.
//
// Deprecated: Use Service().User().ClearUser() instead.
func (r *UserModel) ClearUser(ctx context.Context, user *types.ReviewUser) error {
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Update user status
		_, err := tx.NewUpdate().
			Model(user.User).
			Set("status = ?", enum.UserTypeCleared).
			Where("id = ?", user.ID).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to update user status: %w", err)
		}

		// Create clearance record
		clearance := &types.UserClearance{
			UserID:     user.ID,
			ReviewerID: user.ReviewerID,
			ClearedAt:  time.Now(),
		}
		_, err = tx.NewInsert().
			Model(clearance).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to create clearance record: %w", err)
		}

		return nil
	})
}

// GetConfirmedUsersCount returns the total number of users in confirmed_users.
func (r *UserModel) GetConfirmedUsersCount(ctx context.Context) (int, error) {
	count, err := r.db.NewSelect().
		Model((*types.User)(nil)).
		Where("status = ?", enum.UserTypeConfirmed).
		Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get confirmed users count: %w", err)
	}
	return count, nil
}

// GetFlaggedUsersCount returns the total number of users in flagged_users.
func (r *UserModel) GetFlaggedUsersCount(ctx context.Context) (int, error) {
	count, err := r.db.NewSelect().
		Model((*types.User)(nil)).
		Where("status = ?", enum.UserTypeFlagged).
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

	err := r.db.NewSelect().
		Model((*types.User)(nil)).
		Column("id", "status").
		Where("id IN (?)", bun.In(userIDs)).
		Where("status = ? OR (status IN (?, ?) AND last_updated > NOW() - INTERVAL '7 days')",
			enum.UserTypeConfirmed,
			enum.UserTypeFlagged,
			enum.UserTypeCleared,
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
func (r *UserModel) GetUserByID(
	ctx context.Context, userID string, fields types.UserField,
) (*types.ReviewUser, error) {
	var user types.User
	var result types.ReviewUser

	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Build query
		query := tx.NewSelect().
			Model(&user).
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

		// Get user
		err := query.Scan(ctx)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return types.ErrUserNotFound
			}
			return fmt.Errorf("failed to get user: %w", err)
		}

		// Get verification/clearance info based on status
		result.User = &user

		switch user.Status {
		case enum.UserTypeConfirmed:
			var verification types.UserVerification
			err = tx.NewSelect().
				Model(&verification).
				Where("user_id = ?", user.ID).
				Scan(ctx)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("failed to get verification data: %w", err)
			}
			if err == nil {
				result.ReviewerID = verification.ReviewerID
				result.VerifiedAt = verification.VerifiedAt
			}
		case enum.UserTypeCleared:
			var clearance types.UserClearance
			err = tx.NewSelect().
				Model(&clearance).
				Where("user_id = ?", user.ID).
				Scan(ctx)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("failed to get clearance data: %w", err)
			}
			if err == nil {
				result.ReviewerID = clearance.ReviewerID
				result.ClearedAt = clearance.ClearedAt
			}
		case enum.UserTypeFlagged:
			// Nothing to do here
		}

		// Update last_viewed
		_, err = tx.NewUpdate().
			Model(&user).
			Set("last_viewed = ?", time.Now()).
			Where("id = ?", user.ID).
			Exec(ctx)
		if err != nil {
			return err
		}

		return nil
	})

	return &result, err
}

// GetUsersByIDs retrieves specified user information for a list of user IDs.
// Returns a map of user IDs to review users.
func (r *UserModel) GetUsersByIDs(
	ctx context.Context, userIDs []uint64, fields types.UserField,
) (map[uint64]*types.ReviewUser, error) {
	users := make(map[uint64]*types.ReviewUser)
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Query all users
		var baseUsers []types.User
		err := tx.NewSelect().
			Model(&baseUsers).
			Column(fields.Columns()...).
			Where("id IN (?)", bun.In(userIDs)).
			Scan(ctx)
		if err != nil {
			return fmt.Errorf("failed to get users: %w", err)
		}

		// Get verifications and clearances
		var verifications []types.UserVerification
		err = tx.NewSelect().
			Model(&verifications).
			Where("user_id IN (?)", bun.In(userIDs)).
			Scan(ctx)
		if err != nil {
			return fmt.Errorf("failed to get verifications: %w", err)
		}

		var clearances []types.UserClearance
		err = tx.NewSelect().
			Model(&clearances).
			Where("user_id IN (?)", bun.In(userIDs)).
			Scan(ctx)
		if err != nil {
			return fmt.Errorf("failed to get clearances: %w", err)
		}

		// Map verifications and clearances by user ID
		verificationMap := make(map[uint64]types.UserVerification)
		for _, v := range verifications {
			verificationMap[v.UserID] = v
		}

		clearanceMap := make(map[uint64]types.UserClearance)
		for _, c := range clearances {
			clearanceMap[c.UserID] = c
		}

		// Build review users
		for _, user := range baseUsers {
			reviewUser := &types.ReviewUser{
				User: &user,
			}

			if v, ok := verificationMap[user.ID]; ok {
				reviewUser.ReviewerID = v.ReviewerID
				reviewUser.VerifiedAt = v.VerifiedAt
			}

			if c, ok := clearanceMap[user.ID]; ok {
				reviewUser.ReviewerID = c.ReviewerID
				reviewUser.ClearedAt = c.ClearedAt
			}

			users[user.ID] = reviewUser
		}

		return nil
	})

	return users, err
}

// GetFlaggedAndConfirmedUsers retrieves all flagged and confirmed users.
func (r *UserModel) GetFlaggedAndConfirmedUsers(ctx context.Context) ([]*types.ReviewUser, error) {
	// Get users
	var users []types.User
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		err := tx.NewSelect().
			Model(&users).
			Column("id", "reasons", "confidence", "status").
			Where("status IN (?, ?)", enum.UserTypeFlagged, enum.UserTypeConfirmed).
			Scan(ctx)
		if err != nil {
			return fmt.Errorf("failed to get users: %w", err)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	// Convert to review users
	result := make([]*types.ReviewUser, len(users))
	for i, user := range users {
		result[i] = &types.ReviewUser{
			User: &user,
		}
	}

	return result, nil
}

// GetUsersToCheck finds users that haven't been checked for banned status recently.
func (r *UserModel) GetUsersToCheck(
	ctx context.Context, limit int,
) (userIDs []uint64, bannedIDs []uint64, err error) {
	var users []types.User
	err = r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Get users that need checking
		err := tx.NewSelect().
			Model(&users).
			Column("id", "is_banned").
			Where("status IN (?, ?)", enum.UserTypeConfirmed, enum.UserTypeFlagged).
			Where("last_ban_check < NOW() - INTERVAL '1 day'").
			OrderExpr("last_ban_check ASC").
			Limit(limit).
			For("UPDATE SKIP LOCKED").
			Scan(ctx)
		if err != nil {
			return fmt.Errorf("failed to get users: %w", err)
		}

		if len(users) > 0 {
			userIDs = make([]uint64, 0, len(users))
			for _, user := range users {
				userIDs = append(userIDs, user.ID)
				if user.IsBanned {
					bannedIDs = append(bannedIDs, user.ID)
				}
			}

			// Update last_ban_check
			_, err = tx.NewUpdate().
				Model(&users).
				Set("last_ban_check = NOW()").
				Where("id IN (?)", bun.In(userIDs)).
				Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to update users: %w", err)
			}
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
	_, err := r.db.NewUpdate().
		Model((*types.User)(nil)).
		Set("is_banned = ?", isBanned).
		Where("id IN (?)", bun.In(userIDs)).
		Where("status IN (?, ?)", enum.UserTypeConfirmed, enum.UserTypeFlagged).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to mark users ban status: %w", err)
	}

	r.logger.Debug("Marked users ban status",
		zap.Int("count", len(userIDs)),
		zap.Bool("isBanned", isBanned))
	return nil
}

// GetBannedCount returns the total number of banned users across all tables.
func (r *UserModel) GetBannedCount(ctx context.Context) (int, error) {
	count, err := r.db.NewSelect().
		Model((*types.User)(nil)).
		Where("is_banned = true").
		Where("status IN (?, ?)", enum.UserTypeConfirmed, enum.UserTypeFlagged).
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
		// Get counts by status
		var statusCounts []struct {
			Status enum.UserType
			Count  int
		}
		err := tx.NewSelect().
			Model((*types.User)(nil)).
			Column("status").
			ColumnExpr("COUNT(*) as count").
			Group("status").
			Scan(ctx, &statusCounts)
		if err != nil {
			return fmt.Errorf("failed to get user counts: %w", err)
		}

		// Map counts to their respective fields
		for _, sc := range statusCounts {
			switch sc.Status {
			case enum.UserTypeConfirmed:
				counts.Confirmed = sc.Count
			case enum.UserTypeFlagged:
				counts.Flagged = sc.Count
			case enum.UserTypeCleared:
				counts.Cleared = sc.Count
			}
		}

		// Get banned count
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

// GetOldClearedUsers returns users that were cleared before the cutoff date.
func (r *UserModel) GetOldClearedUsers(ctx context.Context, cutoffDate time.Time) ([]uint64, error) {
	var clearances []types.UserClearance
	err := r.db.NewSelect().
		Model(&clearances).
		Column("user_id").
		Where("cleared_at < ?", cutoffDate).
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get old cleared users: %w", err)
	}

	userIDs := make([]uint64, len(clearances))
	for i, c := range clearances {
		userIDs[i] = c.UserID
	}

	r.logger.Debug("Found old cleared users",
		zap.Int("count", len(userIDs)),
		zap.Time("cutoffDate", cutoffDate))

	return userIDs, nil
}

// GetUsersForThumbnailUpdate retrieves users that need thumbnail updates.
func (r *UserModel) GetUsersForThumbnailUpdate(ctx context.Context, limit int) (map[uint64]*types.User, error) {
	users := make(map[uint64]*types.User)
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var baseUsers []types.User
		err := tx.NewSelect().
			Model(&baseUsers).
			Where("last_thumbnail_update < NOW() - INTERVAL '7 days'").
			Where("is_deleted = false").
			OrderExpr("last_thumbnail_update ASC").
			Limit(limit).
			Scan(ctx)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("failed to query users for thumbnail update: %w", err)
		}

		for _, user := range baseUsers {
			users[user.ID] = &user
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return users, nil
}

// DeleteUsers removes users and their verification/clearance records from the database.
func (r *UserModel) DeleteUsers(ctx context.Context, userIDs []uint64) (int64, error) {
	if len(userIDs) == 0 {
		return 0, nil
	}

	var totalAffected int64
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Delete users
		result, err := tx.NewDelete().
			Model((*types.User)(nil)).
			Where("id IN (?)", bun.In(userIDs)).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to delete users: %w", err)
		}
		affected, _ := result.RowsAffected()
		totalAffected += affected

		// Delete verifications
		result, err = tx.NewDelete().
			Model((*types.UserVerification)(nil)).
			Where("user_id IN (?)", bun.In(userIDs)).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to delete verifications: %w", err)
		}
		affected, _ = result.RowsAffected()
		totalAffected += affected

		// Delete clearances
		result, err = tx.NewDelete().
			Model((*types.UserClearance)(nil)).
			Where("user_id IN (?)", bun.In(userIDs)).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to delete clearances: %w", err)
		}
		affected, _ = result.RowsAffected()
		totalAffected += affected

		return nil
	})
	if err != nil {
		return 0, err
	}

	r.logger.Debug("Deleted users and their core data",
		zap.Int("count", len(userIDs)),
		zap.Int64("affectedRows", totalAffected))

	return totalAffected, nil
}

// DeleteUserGroups removes user group relationships and unreferenced group info.
func (r *UserModel) DeleteUserGroups(ctx context.Context, tx bun.Tx, userIDs []uint64) (int64, error) {
	if len(userIDs) == 0 {
		return 0, nil
	}

	var totalAffected int64

	// Delete unreferenced group info
	result, err := tx.NewDelete().
		Model((*types.GroupInfo)(nil)).
		Where("id IN (SELECT group_id FROM user_groups WHERE user_id IN (?))", bun.In(userIDs)).
		Where("id NOT IN (SELECT group_id FROM user_groups WHERE user_id NOT IN (?))", bun.In(userIDs)).
		Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to delete unreferenced group info: %w", err)
	}
	affected, _ := result.RowsAffected()
	totalAffected += affected

	// Delete user groups
	result, err = tx.NewDelete().
		Model((*types.UserGroup)(nil)).
		Where("user_id IN (?)", bun.In(userIDs)).
		Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to delete user groups: %w", err)
	}
	affected, _ = result.RowsAffected()
	totalAffected += affected

	return totalAffected, nil
}

// DeleteUserOutfits removes user outfit relationships and unreferenced outfits.
func (r *UserModel) DeleteUserOutfits(ctx context.Context, tx bun.Tx, userIDs []uint64) (int64, error) {
	if len(userIDs) == 0 {
		return 0, nil
	}

	var totalAffected int64

	// Delete unreferenced outfits
	result, err := tx.NewDelete().
		Model((*types.UserOutfit)(nil)).
		Where("outfit_id IN (SELECT outfit_id FROM user_outfits WHERE user_id IN (?))", bun.In(userIDs)).
		Where("outfit_id NOT IN (SELECT outfit_id FROM user_outfits WHERE user_id NOT IN (?))", bun.In(userIDs)).
		Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to delete unreferenced outfits: %w", err)
	}
	affected, _ := result.RowsAffected()
	totalAffected += affected

	// Delete user outfits
	result, err = tx.NewDelete().
		Model((*types.UserOutfit)(nil)).
		Where("user_id IN (?)", bun.In(userIDs)).
		Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to delete user outfits: %w", err)
	}
	affected, _ = result.RowsAffected()
	totalAffected += affected

	return totalAffected, nil
}

// DeleteUserFriends removes user friend relationships and unreferenced friend info.
func (r *UserModel) DeleteUserFriends(ctx context.Context, tx bun.Tx, userIDs []uint64) (int64, error) {
	if len(userIDs) == 0 {
		return 0, nil
	}

	var totalAffected int64

	// Delete unreferenced friend info
	result, err := tx.NewDelete().
		Model((*types.FriendInfo)(nil)).
		Where("id IN (SELECT friend_id FROM user_friends WHERE user_id IN (?))", bun.In(userIDs)).
		Where("id NOT IN (SELECT friend_id FROM user_friends WHERE user_id NOT IN (?))", bun.In(userIDs)).
		Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to delete unreferenced friend info: %w", err)
	}
	affected, _ := result.RowsAffected()
	totalAffected += affected

	// Delete user friends
	result, err = tx.NewDelete().
		Model((*types.UserFriend)(nil)).
		Where("user_id IN (?)", bun.In(userIDs)).
		Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to delete user friends: %w", err)
	}
	affected, _ = result.RowsAffected()
	totalAffected += affected

	return totalAffected, nil
}

// DeleteUserGames removes user game relationships and unreferenced game info.
func (r *UserModel) DeleteUserGames(ctx context.Context, tx bun.Tx, userIDs []uint64) (int64, error) {
	if len(userIDs) == 0 {
		return 0, nil
	}

	var totalAffected int64

	// Delete unreferenced game info
	result, err := tx.NewDelete().
		Model((*types.GameInfo)(nil)).
		Where("id IN (SELECT game_id FROM user_games WHERE user_id IN (?))", bun.In(userIDs)).
		Where("id NOT IN (SELECT game_id FROM user_games WHERE user_id NOT IN (?))", bun.In(userIDs)).
		Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to delete unreferenced game info: %w", err)
	}
	affected, _ := result.RowsAffected()
	totalAffected += affected

	// Delete user games
	result, err = tx.NewDelete().
		Model((*types.UserGame)(nil)).
		Where("user_id IN (?)", bun.In(userIDs)).
		Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to delete user games: %w", err)
	}
	affected, _ = result.RowsAffected()
	totalAffected += affected

	return totalAffected, nil
}

// DeleteUserInventory removes user inventory relationships and unreferenced inventory info.
func (r *UserModel) DeleteUserInventory(ctx context.Context, tx bun.Tx, userIDs []uint64) (int64, error) {
	if len(userIDs) == 0 {
		return 0, nil
	}

	var totalAffected int64

	// Delete unreferenced inventory info
	result, err := tx.NewDelete().
		Model((*types.InventoryInfo)(nil)).
		Where("id IN (SELECT inventory_id FROM user_inventories WHERE user_id IN (?))", bun.In(userIDs)).
		Where("id NOT IN (SELECT inventory_id FROM user_inventories WHERE user_id NOT IN (?))", bun.In(userIDs)).
		Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to delete unreferenced inventory info: %w", err)
	}
	affected, _ := result.RowsAffected()
	totalAffected += affected

	// Delete user inventory
	result, err = tx.NewDelete().
		Model((*types.UserInventory)(nil)).
		Where("user_id IN (?)", bun.In(userIDs)).
		Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to delete user inventory: %w", err)
	}
	affected, _ = result.RowsAffected()
	totalAffected += affected

	return totalAffected, nil
}

// GetUserToScan finds the next user to scan.
func (r *UserModel) GetUserToScan(ctx context.Context) (*types.User, error) {
	var user types.User
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// First try confirmed users
		err := tx.NewSelect().Model(&user).
			Where("status = ?", enum.UserTypeConfirmed).
			Where("last_scanned < NOW() - INTERVAL '1 day'").
			OrderExpr("last_scanned ASC, confidence DESC").
			Limit(1).
			For("UPDATE SKIP LOCKED").
			Scan(ctx)

		if err == nil {
			// Update last_scanned
			_, err = tx.NewUpdate().Model(&user).
				Set("last_scanned = ?", time.Now()).
				Where("id = ?", user.ID).
				Exec(ctx)
			if err != nil {
				return fmt.Errorf(
					"failed to update last_scanned for confirmed user: %w (userID=%d)",
					err, user.ID,
				)
			}
			return nil
		}

		if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("failed to query confirmed users: %w", err)
		}

		// If no confirmed users, try flagged users
		err = tx.NewSelect().Model(&user).
			Where("status = ?", enum.UserTypeFlagged).
			Where("last_scanned < NOW() - INTERVAL '1 day'").
			Where("confidence >= 0.8").
			OrderExpr("last_scanned ASC, confidence DESC").
			Limit(1).
			For("UPDATE SKIP LOCKED").
			Scan(ctx)

		if err == nil {
			// Update last_scanned
			_, err = tx.NewUpdate().Model(&user).
				Set("last_scanned = ?", time.Now()).
				Where("id = ?", user.ID).
				Exec(ctx)
			if err != nil {
				return fmt.Errorf(
					"failed to update last_scanned for flagged user: %w (userID=%d)",
					err, user.ID,
				)
			}
			return nil
		}

		if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("failed to query flagged users: %w", err)
		}

		return fmt.Errorf("no users available to scan: %w", err)
	})
	if err != nil {
		return nil, err
	}

	return &user, nil
}

// GetNextToReview handles the common logic for getting the next item to review.
//
// Deprecated: Use Service().User().GetUserToReview() instead.
func (r *UserModel) GetNextToReview(
	ctx context.Context, targetStatus enum.UserType, sortBy enum.ReviewSortBy, recentIDs []uint64,
) (*types.ReviewUser, error) {
	var user types.User
	var result types.ReviewUser

	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Build query
		query := tx.NewSelect().
			Model(&user).
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
			query.Join("LEFT JOIN user_reputations ON user_reputations.id = users.id").
				OrderExpr("COALESCE(user_reputations.score, 0) ASC, last_viewed ASC")
		case enum.ReviewSortByLastViewed:
			query.Order("last_viewed ASC")
		case enum.ReviewSortByRandom:
			query.OrderExpr("RANDOM()")
		}

		query.Limit(1).For("UPDATE")

		// Get user
		err := query.Scan(ctx)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return types.ErrNoUsersToReview
			}
			return err
		}

		result.User = &user

		// Get verification/clearance info based on status
		switch user.Status {
		case enum.UserTypeConfirmed:
			var verification types.UserVerification
			err = tx.NewSelect().
				Model(&verification).
				Where("user_id = ?", user.ID).
				Scan(ctx)
			if err == nil {
				result.ReviewerID = verification.ReviewerID
				result.VerifiedAt = verification.VerifiedAt
			}
		case enum.UserTypeCleared:
			var clearance types.UserClearance
			err = tx.NewSelect().
				Model(&clearance).
				Where("user_id = ?", user.ID).
				Scan(ctx)
			if err == nil {
				result.ReviewerID = clearance.ReviewerID
				result.ClearedAt = clearance.ClearedAt
			}
		case enum.UserTypeFlagged:
			// Nothing to do here
		}

		// Update last_viewed
		_, err = tx.NewUpdate().
			Model(&user).
			Set("last_viewed = ?", time.Now()).
			Where("id = ?", user.ID).
			Exec(ctx)
		if err != nil {
			return err
		}

		return nil
	})

	return &result, err
}

// GetUserGroups fetches groups for a user.
func (r *UserModel) GetUserGroups(ctx context.Context, userID uint64) ([]*apiTypes.UserGroupRoles, error) {
	var results []types.UserGroupQueryResult

	err := r.db.NewSelect().
		TableExpr("user_groups ug").
		Join("JOIN group_infos gi ON gi.id = ug.group_id").
		ColumnExpr("ug.user_id, ug.group_id, ug.role_id, ug.role_name, ug.role_rank, "+
			"gi.name, gi.description, gi.owner, gi.shout, gi.member_count, "+
			"gi.has_verified_badge, gi.is_builders_club_only, gi.public_entry_allowed, gi.is_locked").
		Where("ug.user_id = ?", userID).
		Order("ug.role_rank DESC").
		Scan(ctx, &results)

	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("failed to get user groups: %w", err)
	}

	// Convert to API types
	apiGroups := make([]*apiTypes.UserGroupRoles, len(results))
	for i, result := range results {
		apiGroups[i] = result.ToAPIType()
	}

	return apiGroups, nil
}

// GetUserOutfits fetches outfits for a user.
func (r *UserModel) GetUserOutfits(
	ctx context.Context, userID uint64,
) ([]*apiTypes.Outfit, map[uint64][]*apiTypes.AssetV2, error) {
	var results []types.UserOutfitQueryResult

	err := r.db.NewSelect().
		TableExpr("user_outfits uo").
		Join("JOIN outfit_infos oi ON oi.id = uo.outfit_id").
		ColumnExpr("uo.user_id, uo.outfit_id, oi.name, oi.is_editable, oi.outfit_type").
		Where("uo.user_id = ?", userID).
		Scan(ctx, &results)

	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, nil, fmt.Errorf("failed to get user outfits: %w", err)
	}

	// Get outfit assets
	var outfitAssets []types.OutfitAsset
	err = r.db.NewSelect().
		Model(&outfitAssets).
		Where("outfit_id IN (SELECT outfit_id FROM user_outfits WHERE user_id = ?)", userID).
		Scan(ctx)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, nil, fmt.Errorf("failed to get outfit assets: %w", err)
	}

	// Get asset info
	var assetInfos []types.AssetInfo
	if len(outfitAssets) > 0 {
		assetIDs := make([]uint64, len(outfitAssets))
		for i, asset := range outfitAssets {
			assetIDs[i] = asset.AssetID
		}

		err = r.db.NewSelect().
			Model(&assetInfos).
			Where("id IN (?)", bun.In(assetIDs)).
			Scan(ctx)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, nil, fmt.Errorf("failed to get asset info: %w", err)
		}
	}

	// Build map of outfit ID to assets
	outfitToAssets := make(map[uint64][]*apiTypes.AssetV2)
	assetInfoMap := make(map[uint64]types.AssetInfo)
	for _, info := range assetInfos {
		assetInfoMap[info.ID] = info
	}

	for _, asset := range outfitAssets {
		info, ok := assetInfoMap[asset.AssetID]
		if !ok {
			continue
		}

		outfitToAssets[asset.OutfitID] = append(outfitToAssets[asset.OutfitID], &apiTypes.AssetV2{
			ID:   info.ID,
			Name: info.Name,
			AssetType: apiTypes.AssetType{
				ID: info.AssetType,
			},
			CurrentVersionID: asset.CurrentVersionID,
		})
	}

	// Convert to API types
	apiOutfits := make([]*apiTypes.Outfit, len(results))
	for i, result := range results {
		apiOutfits[i] = result.ToAPIType()
	}

	return apiOutfits, outfitToAssets, nil
}

// GetUserAssets fetches the current assets for a user.
func (r *UserModel) GetUserAssets(ctx context.Context, userID uint64) ([]*apiTypes.AssetV2, error) {
	var results []types.UserAssetQueryResult

	err := r.db.NewSelect().
		TableExpr("user_assets ua").
		Join("JOIN asset_infos ai ON ai.id = ua.asset_id").
		ColumnExpr("ua.user_id, ua.asset_id, ua.current_version_id, "+
			"ai.name, ai.asset_type").
		Where("ua.user_id = ?", userID).
		Scan(ctx, &results)
	if err != nil {
		return nil, fmt.Errorf("failed to get user assets: %w", err)
	}

	// Convert to API types
	assets := make([]*apiTypes.AssetV2, len(results))
	for i, result := range results {
		assets[i] = result.ToAPIType()
	}

	return assets, nil
}

// GetUserFriends fetches friends for a user.
func (r *UserModel) GetUserFriends(ctx context.Context, userID uint64) ([]*apiTypes.ExtendedFriend, error) {
	var results []types.UserFriendQueryResult

	err := r.db.NewSelect().
		TableExpr("user_friends uf").
		Join("JOIN friend_infos fi ON fi.id = uf.friend_id").
		ColumnExpr("uf.user_id, uf.friend_id, fi.name, fi.display_name").
		Where("uf.user_id = ?", userID).
		Scan(ctx, &results)

	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("failed to get user friends: %w", err)
	}

	// Convert to API types
	apiFriends := make([]*apiTypes.ExtendedFriend, len(results))
	for i, result := range results {
		apiFriends[i] = result.ToAPIType()
	}

	return apiFriends, nil
}

// GetFriendInfos retrieves friend information for a list of friend IDs.
// Returns a map of friend IDs to extended friend objects.
func (r *UserModel) GetFriendInfos(ctx context.Context, friendIDs []uint64) (map[uint64]*apiTypes.ExtendedFriend, error) {
	if len(friendIDs) == 0 {
		return make(map[uint64]*apiTypes.ExtendedFriend), nil
	}

	var results []types.FriendInfo
	err := r.db.NewSelect().
		Model((*types.FriendInfo)(nil)).
		Column("id", "name", "display_name").
		Where("id IN (?)", bun.In(friendIDs)).
		Scan(ctx, &results)
	if err != nil {
		return nil, fmt.Errorf("failed to get friend info: %w", err)
	}

	// Convert to map of extended friends
	friendMap := make(map[uint64]*apiTypes.ExtendedFriend, len(results))
	for _, friend := range results {
		friendMap[friend.ID] = &apiTypes.ExtendedFriend{
			Friend: apiTypes.Friend{
				ID: friend.ID,
			},
			Name:        friend.Name,
			DisplayName: friend.DisplayName,
		}
	}

	return friendMap, nil
}

// GetRecentFriendInfos retrieves friend information for a list of friend IDs,
// but only if they were updated within the cutoff time.
func (r *UserModel) GetRecentFriendInfos(
	ctx context.Context, friendIDs []uint64, cutoffTime time.Time,
) (map[uint64]*apiTypes.ExtendedFriend, error) {
	if len(friendIDs) == 0 {
		return make(map[uint64]*apiTypes.ExtendedFriend), nil
	}

	var results []types.FriendInfo
	err := r.db.NewSelect().
		Model((*types.FriendInfo)(nil)).
		Column("id", "name", "display_name").
		Where("id IN (?)", bun.In(friendIDs)).
		Where("last_updated > ?", cutoffTime).
		Scan(ctx, &results)
	if err != nil {
		return nil, fmt.Errorf("failed to get friend info: %w", err)
	}

	// Convert to map of extended friends
	friendMap := make(map[uint64]*apiTypes.ExtendedFriend, len(results))
	for _, friend := range results {
		friendMap[friend.ID] = &apiTypes.ExtendedFriend{
			Friend: apiTypes.Friend{
				ID: friend.ID,
			},
			Name:        friend.Name,
			DisplayName: friend.DisplayName,
		}
	}

	return friendMap, nil
}

// GetUserGames fetches games for a user.
func (r *UserModel) GetUserGames(ctx context.Context, userID uint64) ([]*apiTypes.Game, error) {
	var results []types.UserGameQueryResult

	err := r.db.NewSelect().
		TableExpr("user_games ug").
		Join("JOIN game_infos gi ON gi.id = ug.game_id").
		ColumnExpr("ug.user_id, ug.game_id, "+
			"gi.name, gi.description, gi.place_visits, gi.created, gi.updated").
		Where("ug.user_id = ?", userID).
		Order("gi.place_visits DESC").
		Scan(ctx, &results)

	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("failed to get user games: %w", err)
	}

	// Convert to API types
	apiGames := make([]*apiTypes.Game, len(results))
	for i, result := range results {
		apiGames[i] = result.ToAPIType()
	}

	return apiGames, nil
}

// GetUserInventory fetches inventory for a user.
func (r *UserModel) GetUserInventory(ctx context.Context, userID uint64) ([]*apiTypes.InventoryAsset, error) {
	var results []types.UserInventoryQueryResult

	err := r.db.NewSelect().
		TableExpr("user_inventories ui").
		Join("JOIN inventory_infos ii ON ii.id = ui.inventory_id").
		ColumnExpr("ui.user_id, ui.inventory_id, ii.name, ii.asset_type, ii.created").
		Where("ui.user_id = ?", userID).
		Scan(ctx, &results)

	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("failed to get user inventory: %w", err)
	}

	// Convert to API types
	apiInventory := make([]*apiTypes.InventoryAsset, len(results))
	for i, result := range results {
		apiInventory[i] = result.ToAPIType()
	}

	return apiInventory, nil
}

// SaveUserGroups saves groups for multiple users.
func (r *UserModel) SaveUserGroups(ctx context.Context, tx bun.Tx, userGroups map[uint64][]*apiTypes.UserGroupRoles) error {
	if len(userGroups) == 0 {
		return nil
	}

	// Calculate total size for slices
	totalGroups := 0
	for _, groups := range userGroups {
		totalGroups += len(groups)
	}

	// Pre-allocate slices
	allUserGroups := make([]types.UserGroup, 0, totalGroups)
	groupInfoMap := make(map[uint64]*types.GroupInfo)

	// Build user groups and group info
	for userID, groups := range userGroups {
		for _, group := range groups {
			userGroup, groupInfo := types.FromAPIGroupRoles(userID, group)
			allUserGroups = append(allUserGroups, *userGroup)
			groupInfoMap[group.Group.ID] = groupInfo
		}
	}

	// Convert group info map to slice
	groupInfos := make([]types.GroupInfo, 0, len(groupInfoMap))
	for _, info := range groupInfoMap {
		groupInfos = append(groupInfos, *info)
	}

	// Save group info
	_, err := tx.NewInsert().
		Model(&groupInfos).
		On("CONFLICT (id) DO UPDATE").
		Set("name = EXCLUDED.name").
		Set("description = EXCLUDED.description").
		Set("owner = EXCLUDED.owner").
		Set("shout = EXCLUDED.shout").
		Set("member_count = EXCLUDED.member_count").
		Set("is_builders_club_only = EXCLUDED.is_builders_club_only").
		Set("public_entry_allowed = EXCLUDED.public_entry_allowed").
		Set("is_locked = EXCLUDED.is_locked").
		Set("has_verified_badge = EXCLUDED.has_verified_badge").
		Set("last_updated = EXCLUDED.last_updated").
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to upsert group info: %w", err)
	}

	// Save user groups
	_, err = tx.NewInsert().
		Model(&allUserGroups).
		On("CONFLICT (user_id, group_id) DO UPDATE").
		Set("role_id = EXCLUDED.role_id").
		Set("role_name = EXCLUDED.role_name").
		Set("role_rank = EXCLUDED.role_rank").
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to upsert user groups: %w", err)
	}

	return nil
}

// SaveUserOutfits saves outfits and their assets for multiple users.
func (r *UserModel) SaveUserOutfits(
	ctx context.Context, tx bun.Tx, userOutfits map[uint64][]*apiTypes.Outfit,
	userOutfitAssets map[uint64]map[uint64][]*apiTypes.AssetV2,
) error {
	if len(userOutfits) == 0 {
		return nil
	}

	// Calculate total size for slices
	totalOutfits := 0
	for _, outfits := range userOutfits {
		totalOutfits += len(outfits)
	}

	// Pre-allocate slices
	allUserOutfits := make([]types.UserOutfit, 0, totalOutfits)
	outfitInfoMap := make(map[uint64]*types.OutfitInfo)

	// Build user outfits and outfit info
	for userID, outfits := range userOutfits {
		for _, outfit := range outfits {
			userOutfit, outfitInfo := types.FromAPIOutfit(userID, outfit)
			allUserOutfits = append(allUserOutfits, *userOutfit)
			outfitInfoMap[outfit.ID] = outfitInfo
		}
	}

	// Convert outfit info map to slice
	outfitInfos := make([]types.OutfitInfo, 0, len(outfitInfoMap))
	for _, info := range outfitInfoMap {
		outfitInfos = append(outfitInfos, *info)
	}

	// Save outfit info
	_, err := tx.NewInsert().
		Model(&outfitInfos).
		On("CONFLICT (id) DO UPDATE").
		Set("name = EXCLUDED.name").
		Set("is_editable = EXCLUDED.is_editable").
		Set("outfit_type = EXCLUDED.outfit_type").
		Set("last_updated = EXCLUDED.last_updated").
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to upsert outfit info: %w", err)
	}

	// Save user outfits
	_, err = tx.NewInsert().
		Model(&allUserOutfits).
		On("CONFLICT (user_id, outfit_id) DO UPDATE").
		Set("user_id = EXCLUDED.user_id").
		Set("outfit_id = EXCLUDED.outfit_id").
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to upsert user outfits: %w", err)
	}

	// Save outfit assets if provided
	if len(userOutfitAssets) > 0 {
		var (
			assets   []types.OutfitAsset
			assetMap = make(map[uint64]types.AssetInfo)
		)

		// Prepare asset data
		for _, outfitAssets := range userOutfitAssets {
			for outfitID, assetList := range outfitAssets {
				for _, asset := range assetList {
					assets = append(assets, types.OutfitAsset{
						OutfitID:         outfitID,
						AssetID:          asset.ID,
						CurrentVersionID: asset.CurrentVersionID,
					})

					assetMap[asset.ID] = types.AssetInfo{
						ID:        asset.ID,
						Name:      asset.Name,
						AssetType: asset.AssetType.ID,
					}
				}
			}
		}

		// Convert map to slice for insertion
		assetInfo := make([]types.AssetInfo, 0, len(assetMap))
		for _, info := range assetMap {
			assetInfo = append(assetInfo, info)
		}

		// Save asset info
		_, err = tx.NewInsert().
			Model(&assetInfo).
			On("CONFLICT (id) DO UPDATE").
			Set("name = EXCLUDED.name").
			Set("asset_type = EXCLUDED.asset_type").
			Set("last_updated = EXCLUDED.last_updated").
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to upsert asset info: %w", err)
		}

		// Save outfit assets
		_, err = tx.NewInsert().
			Model(&assets).
			On("CONFLICT (outfit_id, asset_id) DO UPDATE").
			Set("current_version_id = EXCLUDED.current_version_id").
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to upsert outfit assets: %w", err)
		}
	}

	return nil
}

// SaveUserAssets saves the current assets for multiple users.
func (r *UserModel) SaveUserAssets(ctx context.Context, tx bun.Tx, userAssets map[uint64][]*apiTypes.AssetV2) error {
	if len(userAssets) == 0 {
		return nil
	}

	// Calculate total size for slices
	totalAssets := 0
	for _, assets := range userAssets {
		totalAssets += len(assets)
	}

	// Pre-allocate slices
	allUserAssets := make([]types.UserAsset, 0, totalAssets)
	assetInfoMap := make(map[uint64]*types.AssetInfo)

	// Build user assets and asset info
	for userID, assets := range userAssets {
		for _, asset := range assets {
			userAsset, assetInfo := types.FromAPIAsset(userID, asset)
			allUserAssets = append(allUserAssets, *userAsset)
			assetInfoMap[asset.ID] = assetInfo
		}
	}

	// Convert asset info map to slice
	assetInfos := make([]types.AssetInfo, 0, len(assetInfoMap))
	for _, info := range assetInfoMap {
		assetInfos = append(assetInfos, *info)
	}

	// Save asset info
	_, err := tx.NewInsert().
		Model(&assetInfos).
		On("CONFLICT (id) DO UPDATE").
		Set("name = EXCLUDED.name").
		Set("asset_type = EXCLUDED.asset_type").
		Set("last_updated = EXCLUDED.last_updated").
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to upsert asset info: %w", err)
	}

	// Save user assets
	_, err = tx.NewInsert().
		Model(&allUserAssets).
		On("CONFLICT (user_id, asset_id) DO UPDATE").
		Set("current_version_id = EXCLUDED.current_version_id").
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to upsert user assets: %w", err)
	}

	return nil
}

// SaveUserFriends saves friends for multiple users.
func (r *UserModel) SaveUserFriends(ctx context.Context, tx bun.Tx, userFriends map[uint64][]*apiTypes.ExtendedFriend) error {
	if len(userFriends) == 0 {
		return nil
	}

	// Calculate total size for slices
	totalFriends := 0
	for _, friends := range userFriends {
		totalFriends += len(friends)
	}

	// Pre-allocate slices
	allUserFriends := make([]types.UserFriend, 0, totalFriends)
	friendInfoMap := make(map[uint64]*types.FriendInfo)

	// Build user friends and friend info
	for userID, friends := range userFriends {
		for _, friend := range friends {
			userFriend, friendInfo := types.FromAPIFriend(userID, friend)
			allUserFriends = append(allUserFriends, *userFriend)
			friendInfoMap[friend.ID] = friendInfo
		}
	}

	// Convert friend info map to slice
	friendInfos := make([]types.FriendInfo, 0, len(friendInfoMap))
	for _, info := range friendInfoMap {
		friendInfos = append(friendInfos, *info)
	}

	// Save friend info
	_, err := tx.NewInsert().
		Model(&friendInfos).
		On("CONFLICT (id) DO UPDATE").
		Set("name = EXCLUDED.name").
		Set("display_name = EXCLUDED.display_name").
		Set("last_updated = EXCLUDED.last_updated").
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to upsert friend info: %w", err)
	}

	// Save user friends
	_, err = tx.NewInsert().
		Model(&allUserFriends).
		On("CONFLICT (user_id, friend_id) DO NOTHING").
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to upsert user friends: %w", err)
	}

	return nil
}

// SaveUserGames saves games for multiple users.
func (r *UserModel) SaveUserGames(ctx context.Context, tx bun.Tx, userGames map[uint64][]*apiTypes.Game) error {
	if len(userGames) == 0 {
		return nil
	}

	// Calculate total size for slices
	totalGames := 0
	for _, games := range userGames {
		totalGames += len(games)
	}

	// Pre-allocate slices
	allUserGames := make([]types.UserGame, 0, totalGames)
	gameInfoMap := make(map[uint64]*types.GameInfo)

	// Build user games and game info
	for userID, games := range userGames {
		for _, game := range games {
			userGame, gameInfo := types.FromAPIGame(userID, game)
			allUserGames = append(allUserGames, *userGame)
			gameInfoMap[game.ID] = gameInfo
		}
	}

	// Convert game info map to slice
	gameInfos := make([]types.GameInfo, 0, len(gameInfoMap))
	for _, info := range gameInfoMap {
		gameInfos = append(gameInfos, *info)
	}

	// Save game info
	_, err := tx.NewInsert().
		Model(&gameInfos).
		On("CONFLICT (id) DO UPDATE").
		Set("name = EXCLUDED.name").
		Set("description = EXCLUDED.description").
		Set("place_visits = EXCLUDED.place_visits").
		Set("created = EXCLUDED.created").
		Set("updated = EXCLUDED.updated").
		Set("last_updated = EXCLUDED.last_updated").
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to upsert game info: %w", err)
	}

	// Save user games
	_, err = tx.NewInsert().
		Model(&allUserGames).
		On("CONFLICT (user_id, game_id) DO NOTHING").
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to upsert user games: %w", err)
	}

	return nil
}

// SaveUserInventory saves inventory for multiple users.
func (r *UserModel) SaveUserInventory(ctx context.Context, tx bun.Tx, userInventory map[uint64][]*apiTypes.InventoryAsset) error {
	if len(userInventory) == 0 {
		return nil
	}

	// Calculate total size for slices
	totalInventory := 0
	for _, inventory := range userInventory {
		totalInventory += len(inventory)
	}

	// Pre-allocate slices
	allUserInventory := make([]types.UserInventory, 0, totalInventory)
	inventoryInfoMap := make(map[uint64]*types.InventoryInfo)

	// Build user inventory and inventory info
	for userID, inventory := range userInventory {
		for _, asset := range inventory {
			userInv, invInfo := types.FromAPIInventoryAsset(userID, asset)
			allUserInventory = append(allUserInventory, *userInv)
			inventoryInfoMap[asset.AssetID] = invInfo
		}
	}

	// Convert inventory info map to slice
	inventoryInfos := make([]types.InventoryInfo, 0, len(inventoryInfoMap))
	for _, info := range inventoryInfoMap {
		inventoryInfos = append(inventoryInfos, *info)
	}

	// Save inventory info
	_, err := tx.NewInsert().
		Model(&inventoryInfos).
		On("CONFLICT (id) DO UPDATE").
		Set("name = EXCLUDED.name").
		Set("asset_type = EXCLUDED.asset_type").
		Set("created = EXCLUDED.created").
		Set("last_updated = EXCLUDED.last_updated").
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to upsert inventory info: %w", err)
	}

	// Save user inventory
	_, err = tx.NewInsert().
		Model(&allUserInventory).
		On("CONFLICT (user_id, inventory_id) DO NOTHING").
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to upsert user inventory: %w", err)
	}

	return nil
}

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

// SaveUsers saves users to the database.
//
// Deprecated: Use Service().User().SaveUsers() instead.
func (r *UserModel) SaveUsers(ctx context.Context, users []*types.User) error {
	if len(users) == 0 {
		return nil
	}

	// Update users table
	_, err := r.db.NewInsert().
		Model(&users).
		On("CONFLICT (id) DO UPDATE").
		Set("uuid = EXCLUDED.uuid").
		Set("name = EXCLUDED.name").
		Set("display_name = EXCLUDED.display_name").
		Set("description = EXCLUDED.description").
		Set("created_at = EXCLUDED.created_at").
		Set("status = EXCLUDED.status").
		Set("reasons = EXCLUDED.reasons").
		Set("groups = EXCLUDED.groups").
		Set("outfits = EXCLUDED.outfits").
		Set("friends = EXCLUDED.friends").
		Set("games = EXCLUDED.games").
		Set("inventory = EXCLUDED.inventory").
		Set("favorites = EXCLUDED.favorites").
		Set("badges = EXCLUDED.badges").
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
		result.Status = user.Status

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
				User:   &user,
				Status: user.Status,
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
			User:   &user,
			Status: user.Status,
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

// PurgeOldClearedUsers removes cleared users older than the cutoff date.
func (r *UserModel) PurgeOldClearedUsers(ctx context.Context, cutoffDate time.Time) (int, error) {
	var clearances []types.UserClearance
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Get users to delete
		err := tx.NewSelect().
			Model(&clearances).
			Where("cleared_at < ?", cutoffDate).
			Scan(ctx)
		if err != nil {
			return fmt.Errorf("failed to get old cleared users: %w", err)
		}

		if len(clearances) > 0 {
			userIDs := make([]uint64, len(clearances))
			for i, c := range clearances {
				userIDs[i] = c.UserID
			}

			// Delete users
			_, err = tx.NewDelete().
				Model((*types.User)(nil)).
				Where("id IN (?)", bun.In(userIDs)).
				Where("status = ?", enum.UserTypeCleared).
				Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to delete old cleared users: %w", err)
			}

			// Delete clearances
			_, err = tx.NewDelete().
				Model(&clearances).
				Where("user_id IN (?)", bun.In(userIDs)).
				Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to delete old clearances: %w", err)
			}
		}

		return nil
	})
	if err != nil {
		return 0, err
	}

	r.logger.Debug("Purged old cleared users",
		zap.Int("count", len(clearances)),
		zap.Time("cutoffDate", cutoffDate))

	return len(clearances), nil
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

// DeleteUser removes a user and all associated data from the database.
func (r *UserModel) DeleteUser(ctx context.Context, userID uint64) (bool, error) {
	var totalAffected int64
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Delete user
		result, err := tx.NewDelete().
			Model((*types.User)(nil)).
			Where("id = ?", userID).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to delete user: %w", err)
		}
		affected, _ := result.RowsAffected()
		totalAffected += affected

		// Delete verification if exists
		result, err = tx.NewDelete().
			Model((*types.UserVerification)(nil)).
			Where("user_id = ?", userID).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to delete verification: %w", err)
		}
		affected, _ = result.RowsAffected()
		totalAffected += affected

		// Delete clearance if exists
		result, err = tx.NewDelete().
			Model((*types.UserClearance)(nil)).
			Where("user_id = ?", userID).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to delete clearance: %w", err)
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
		result.Status = user.Status

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

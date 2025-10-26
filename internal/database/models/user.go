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
	"github.com/robalyx/rotector/internal/database/dbretry"
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

	// Extract base users
	baseUsers := make([]*types.User, len(users))
	for i, user := range users {
		baseUsers[i] = user.User
	}

	// Update users table with core data
	_, err := tx.NewInsert().
		Model(&baseUsers).
		On("CONFLICT (id) DO UPDATE").
		Set("uuid = EXCLUDED.uuid").
		Set("name = EXCLUDED.name").
		Set("display_name = EXCLUDED.display_name").
		Set("description = EXCLUDED.description").
		Set("created_at = EXCLUDED.created_at").
		Set("status = EXCLUDED.status").
		Set("confidence = EXCLUDED.confidence").
		Set("engine_version = EXCLUDED.engine_version").
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

	// Delete existing reasons for these users to ensure complete replacement
	if len(users) > 0 {
		userIDs := make([]int64, len(users))
		for i, user := range users {
			userIDs[i] = user.ID
		}

		_, err = tx.NewDelete().
			Model((*types.UserReason)(nil)).
			Where("user_id IN (?)", bun.In(userIDs)).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to delete old user reasons: %w", err)
		}
	}

	// Save user reasons
	var reasons []*types.UserReason

	for _, user := range users {
		if user.Reasons != nil {
			for reasonType, reason := range user.Reasons {
				reasons = append(reasons, &types.UserReason{
					UserID:     user.ID,
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
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to insert user reasons: %w", err)
		}
	}

	return nil
}

// ConfirmUsers moves multiple users to confirmed status and creates verification records.
func (r *UserModel) ConfirmUsers(ctx context.Context, users []*types.ReviewUser) error {
	if len(users) == 0 {
		return nil
	}

	return dbretry.Transaction(ctx, r.db, func(ctx context.Context, tx bun.Tx) error {
		// Extract user IDs
		userIDs := make([]int64, len(users))
		for i, user := range users {
			userIDs[i] = user.ID
		}

		// Batch delete existing clearance records
		_, err := tx.NewDelete().
			Model((*types.UserClearance)(nil)).
			Where("user_id IN (?)", bun.In(userIDs)).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to delete existing clearance records: %w", err)
		}

		// Update user statuses and categories
		for _, user := range users {
			_, err = tx.NewUpdate().
				Model((*types.User)(nil)).
				Set("status = ?", enum.UserTypeConfirmed).
				Set("category = ?", user.Category).
				Where("id = ?", user.ID).
				Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to update user status and category: %w", err)
			}
		}

		// Prepare verification records
		verifications := make([]*types.UserVerification, len(users))
		for i, user := range users {
			verifications[i] = &types.UserVerification{
				UserID:     user.ID,
				ReviewerID: user.ReviewerID,
				VerifiedAt: time.Now(),
			}
		}

		// Batch insert verification records
		_, err = tx.NewInsert().
			Model(&verifications).
			On("CONFLICT (user_id) DO UPDATE").
			Set("reviewer_id = EXCLUDED.reviewer_id").
			Set("verified_at = EXCLUDED.verified_at").
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to create verification records: %w", err)
		}

		// Batch update user reasons
		var allReasons []*types.UserReason

		for _, user := range users {
			if user.Reasons != nil {
				for reasonType, reason := range user.Reasons {
					allReasons = append(allReasons, &types.UserReason{
						UserID:     user.ID,
						ReasonType: reasonType,
						Message:    reason.Message,
						Confidence: reason.Confidence,
						Evidence:   reason.Evidence,
						CreatedAt:  time.Now(),
					})
				}
			}
		}

		if len(allReasons) > 0 {
			_, err = tx.NewInsert().
				Model(&allReasons).
				On("CONFLICT (user_id, reason_type) DO UPDATE").
				Set("message = EXCLUDED.message").
				Set("confidence = EXCLUDED.confidence").
				Set("evidence = EXCLUDED.evidence").
				Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to update user reasons: %w", err)
			}
		}

		return nil
	})
}

// ClearUser moves a user to cleared status and creates a clearance record.
//
// Deprecated: Use Service().User().ClearUser() instead.
func (r *UserModel) ClearUser(ctx context.Context, user *types.ReviewUser) error {
	return dbretry.Transaction(ctx, r.db, func(ctx context.Context, tx bun.Tx) error {
		return r.ClearUserWithTx(ctx, tx, user)
	})
}

// ClearUserWithTx moves a user to cleared status and creates a clearance record using the provided transaction.
//
// Deprecated: Use Service().User().ClearUserWithTx() instead.
func (r *UserModel) ClearUserWithTx(ctx context.Context, tx bun.Tx, user *types.ReviewUser) error {
	// Delete any existing verification record
	_, err := tx.NewDelete().
		Model((*types.UserVerification)(nil)).
		Where("user_id = ?", user.ID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete existing verification record: %w", err)
	}

	// Update user status
	_, err = tx.NewUpdate().
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
		On("CONFLICT (user_id) DO UPDATE").
		Set("reviewer_id = EXCLUDED.reviewer_id").
		Set("cleared_at = EXCLUDED.cleared_at").
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to create clearance record: %w", err)
	}

	// Update user reasons if any exist
	if user.Reasons != nil {
		var reasons []*types.UserReason
		for reasonType, reason := range user.Reasons {
			reasons = append(reasons, &types.UserReason{
				UserID:     user.ID,
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
				On("CONFLICT (user_id, reason_type) DO UPDATE").
				Set("message = EXCLUDED.message").
				Set("confidence = EXCLUDED.confidence").
				Set("evidence = EXCLUDED.evidence").
				Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to update user reasons: %w", err)
			}
		}
	}

	return nil
}

// UpdateUsersToPastOffender updates users to past offender status and deletes their reasons.
func (r *UserModel) UpdateUsersToPastOffender(ctx context.Context, tx bun.Tx, userIDs []int64) error {
	if len(userIDs) == 0 {
		return nil
	}

	// Delete user reasons
	_, err := tx.NewDelete().
		Model((*types.UserReason)(nil)).
		Where("user_id IN (?)", bun.In(userIDs)).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete user reasons: %w", err)
	}

	// Update user status to past offender
	_, err = tx.NewUpdate().
		Model((*types.User)(nil)).
		Set("status = ?", enum.UserTypePastOffender).
		Set("confidence = 0").
		Set("last_updated = ?", time.Now()).
		Where("id IN (?)", bun.In(userIDs)).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to update users to past offender status: %w", err)
	}

	r.logger.Debug("Updated users to past offender status",
		zap.Int("count", len(userIDs)))

	return nil
}

// GetConfirmedUsersCount returns the total number of users in confirmed_users.
func (r *UserModel) GetConfirmedUsersCount(ctx context.Context) (int, error) {
	count, err := dbretry.Operation(ctx, func(ctx context.Context) (int, error) {
		return r.db.NewSelect().
			Model((*types.User)(nil)).
			Where("status = ?", enum.UserTypeConfirmed).
			Count(ctx)
	})
	if err != nil {
		return 0, fmt.Errorf("failed to get confirmed users count: %w", err)
	}

	return count, nil
}

// GetFlaggedUsersCount returns the total number of users in flagged_users.
func (r *UserModel) GetFlaggedUsersCount(ctx context.Context) (int, error) {
	count, err := dbretry.Operation(ctx, func(ctx context.Context) (int, error) {
		return r.db.NewSelect().
			Model((*types.User)(nil)).
			Where("status = ?", enum.UserTypeFlagged).
			Count(ctx)
	})
	if err != nil {
		return 0, fmt.Errorf("failed to get flagged users count: %w", err)
	}

	return count, nil
}

// GetUserByID retrieves a user by either their numeric ID or UUID.
//
// Deprecated: Use Service().User().GetUserByID() instead.
func (r *UserModel) GetUserByID(
	ctx context.Context, userID string, fields types.UserField,
) (*types.ReviewUser, error) {
	var (
		user   types.User
		result types.ReviewUser
	)

	err := dbretry.Transaction(ctx, r.db, func(ctx context.Context, tx bun.Tx) error {
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

		// Get user reasons
		var reasons []*types.UserReason

		err = tx.NewSelect().
			Model(&reasons).
			Where("user_id = ?", user.ID).
			Scan(ctx)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("failed to get user reasons: %w", err)
		}

		// Convert reasons to map
		result.Reasons = make(types.Reasons[enum.UserReasonType])
		for _, reason := range reasons {
			result.Reasons[reason.ReasonType] = &types.Reason{
				Message:    reason.Message,
				Confidence: reason.Confidence,
				Evidence:   reason.Evidence,
			}
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
			// No additional data to load for flagged users
		case enum.UserTypeQueued, enum.UserTypeBloxDB, enum.UserTypeMixed, enum.UserTypePastOffender:
			// No verification/clearance data for these statuses
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
	ctx context.Context, userIDs []int64, fields types.UserField,
) (map[int64]*types.ReviewUser, error) {
	users := make(map[int64]*types.ReviewUser)
	err := dbretry.Transaction(ctx, r.db, func(ctx context.Context, tx bun.Tx) error {
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

		// Get user reasons
		var reasons []*types.UserReason
		if fields.Has(types.UserFieldReasons) {
			err = tx.NewSelect().
				Model(&reasons).
				Where("user_id IN (?)", bun.In(userIDs)).
				Scan(ctx)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("failed to get user reasons: %w", err)
			}
		}

		// Map verifications and clearances by user ID
		verificationMap := make(map[int64]types.UserVerification)
		for _, v := range verifications {
			verificationMap[v.UserID] = v
		}

		clearanceMap := make(map[int64]types.UserClearance)
		for _, c := range clearances {
			clearanceMap[c.UserID] = c
		}

		// Map reasons by user ID
		reasonMap := make(map[int64]types.Reasons[enum.UserReasonType])
		for _, reason := range reasons {
			if _, ok := reasonMap[reason.UserID]; !ok {
				reasonMap[reason.UserID] = make(types.Reasons[enum.UserReasonType])
			}

			reasonMap[reason.UserID][reason.ReasonType] = &types.Reason{
				Message:    reason.Message,
				Confidence: reason.Confidence,
				Evidence:   reason.Evidence,
			}
		}

		// Build review users
		for _, user := range baseUsers {
			reviewUser := &types.ReviewUser{
				User:    &user,
				Reasons: reasonMap[user.ID],
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

	err := dbretry.Transaction(ctx, r.db, func(ctx context.Context, tx bun.Tx) error {
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

// GetUsersToCheck finds unbanned users that haven't been checked for banned status recently.
func (r *UserModel) GetUsersToCheck(
	ctx context.Context, limit int,
) ([]int64, error) {
	var userIDs []int64

	err := dbretry.Transaction(ctx, r.db, func(ctx context.Context, tx bun.Tx) error {
		// Get users that need ban status checking
		err := tx.NewSelect().
			Model((*types.User)(nil)).
			Column("id").
			Where("status IN (?, ?)", enum.UserTypeConfirmed, enum.UserTypeFlagged).
			Where("is_banned = false").
			Where("last_ban_check < NOW() - INTERVAL '1 day'").
			OrderExpr("last_ban_check ASC").
			Limit(limit).
			For("UPDATE SKIP LOCKED").
			Scan(ctx, &userIDs)
		if err != nil {
			return fmt.Errorf("failed to get users: %w", err)
		}

		if len(userIDs) > 0 {
			// Update last_ban_check
			_, err = tx.NewUpdate().
				Model((*types.User)(nil)).
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
		return nil, err
	}

	return userIDs, nil
}

// GetUsersForGroupCheck finds flagged/confirmed users that haven't had their groups checked recently.
func (r *UserModel) GetUsersForGroupCheck(
	ctx context.Context, limit int,
) ([]int64, error) {
	var userIDs []int64

	err := dbretry.Transaction(ctx, r.db, func(ctx context.Context, tx bun.Tx) error {
		// Get users that need group checking
		err := tx.NewSelect().
			Model((*types.User)(nil)).
			Column("id").
			Where("status IN (?, ?)", enum.UserTypeConfirmed, enum.UserTypeFlagged).
			Where("is_banned = false").
			Where("last_group_check < NOW() - INTERVAL '3 days'").
			OrderExpr("last_group_check ASC").
			Limit(limit).
			For("UPDATE SKIP LOCKED").
			Scan(ctx, &userIDs)
		if err != nil {
			return fmt.Errorf("failed to get users: %w", err)
		}

		if len(userIDs) > 0 {
			// Update last_group_check
			_, err = tx.NewUpdate().
				Model((*types.User)(nil)).
				Set("last_group_check = NOW()").
				Where("id IN (?)", bun.In(userIDs)).
				Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to update users: %w", err)
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return userIDs, nil
}

// MarkUsersBanStatus updates the banned status of users in their respective tables.
func (r *UserModel) MarkUsersBanStatus(ctx context.Context, userIDs []int64, isBanned bool) error {
	err := dbretry.NoResult(ctx, func(ctx context.Context) error {
		_, err := r.db.NewUpdate().
			Model((*types.User)(nil)).
			Set("is_banned = ?", isBanned).
			Where("id IN (?)", bun.In(userIDs)).
			Where("status IN (?, ?)", enum.UserTypeConfirmed, enum.UserTypeFlagged).
			Exec(ctx)

		return err
	})
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
	count, err := dbretry.Operation(ctx, func(ctx context.Context) (int, error) {
		return r.db.NewSelect().
			Model((*types.User)(nil)).
			Where("is_banned = true").
			Where("status IN (?, ?)", enum.UserTypeConfirmed, enum.UserTypeFlagged).
			Count(ctx)
	})
	if err != nil {
		return 0, err
	}

	return count, nil
}

// GetUserCounts returns counts for all user statuses.
func (r *UserModel) GetUserCounts(ctx context.Context) (*types.UserCounts, error) {
	var counts types.UserCounts

	err := dbretry.Transaction(ctx, r.db, func(ctx context.Context, tx bun.Tx) error {
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
			case enum.UserTypeQueued, enum.UserTypeBloxDB, enum.UserTypeMixed, enum.UserTypePastOffender:
				// These statuses are not tracked in the counts struct
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
func (r *UserModel) GetOldClearedUsers(ctx context.Context, cutoffDate time.Time) ([]int64, error) {
	var clearances []types.UserClearance

	err := dbretry.NoResult(ctx, func(ctx context.Context) error {
		return r.db.NewSelect().
			Model(&clearances).
			Column("user_id").
			Where("cleared_at < ?", cutoffDate).
			Scan(ctx)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get old cleared users: %w", err)
	}

	userIDs := make([]int64, len(clearances))
	for i, c := range clearances {
		userIDs[i] = c.UserID
	}

	r.logger.Debug("Found old cleared users",
		zap.Int("count", len(userIDs)),
		zap.Time("cutoffDate", cutoffDate))

	return userIDs, nil
}

// GetUsersForThumbnailUpdate retrieves users that need thumbnail updates.
func (r *UserModel) GetUsersForThumbnailUpdate(ctx context.Context, limit int) (map[int64]*types.User, error) {
	users := make(map[int64]*types.User)

	err := dbretry.Transaction(ctx, r.db, func(ctx context.Context, tx bun.Tx) error {
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

// GetFlaggedUsersWithOnlyReason returns users that are flagged, have only the specified reason type,
// and have confidence below the specified threshold for that specific reason.
func (r *UserModel) GetFlaggedUsersWithOnlyReason(
	ctx context.Context, reasonType enum.UserReasonType, confidenceThreshold float64,
) ([]*types.User, error) {
	var users []*types.User

	err := dbretry.NoResult(ctx, func(ctx context.Context) error {
		return r.db.NewSelect().
			Model(&users).
			Where("status = ?", enum.UserTypeFlagged).
			Where("id IN (SELECT user_id FROM user_reasons WHERE reason_type = ? AND confidence < ? AND "+
				"user_id NOT IN (SELECT user_id FROM user_reasons WHERE reason_type != ?))", reasonType, confidenceThreshold, reasonType).
			Order("id ASC").
			Scan(ctx)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get users with only reason %s and reason confidence < %.2f: %w",
			reasonType, confidenceThreshold, err)
	}

	r.logger.Debug("Found users with only reason and below reason confidence threshold",
		zap.String("reason", reasonType.String()),
		zap.Float64("confidenceThreshold", confidenceThreshold),
		zap.Int("count", len(users)))

	return users, nil
}

// GetFlaggedUsersWithProfileReasons returns user IDs of flagged users that have profile reasons
// with confidence above the specified threshold.
//
// Deprecated: Use Service().User().GetFlaggedUsersWithProfileReasons() instead.
func (r *UserModel) GetFlaggedUsersWithProfileReasons(
	ctx context.Context, confidenceThreshold float64, limit int,
) ([]int64, error) {
	var userIDs []int64

	err := dbretry.NoResult(ctx, func(ctx context.Context) error {
		query := r.db.NewSelect().
			Distinct().
			Column("user_id").
			Table("user_reasons").
			Where("reason_type = ?", enum.UserReasonTypeProfile).
			Where("confidence >= ?", confidenceThreshold).
			Where("user_id IN (SELECT id FROM users WHERE status = ?)", enum.UserTypeFlagged).
			Order("user_id ASC")

		if limit > 0 {
			query = query.Limit(limit)
		}

		return query.Scan(ctx, &userIDs)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get user IDs with profile reasons (confidence >= %.2f): %w",
			confidenceThreshold, err)
	}

	r.logger.Debug("Found user IDs with profile reasons",
		zap.Float64("confidenceThreshold", confidenceThreshold),
		zap.Int("limit", limit),
		zap.Int("count", len(userIDs)))

	return userIDs, nil
}

// DeleteUsers removes users and their verification/clearance records from the database.
func (r *UserModel) DeleteUsers(ctx context.Context, userIDs []int64) (int64, error) {
	if len(userIDs) == 0 {
		return 0, nil
	}

	var totalAffected int64

	err := dbretry.Transaction(ctx, r.db, func(ctx context.Context, tx bun.Tx) error {
		var err error

		totalAffected, err = r.DeleteUsersWithTx(ctx, tx, userIDs)

		return err
	})
	if err != nil {
		return 0, err
	}

	return totalAffected, nil
}

// DeleteUsersWithTx removes users and their verification/clearance records from the database using the provided transaction.
func (r *UserModel) DeleteUsersWithTx(ctx context.Context, tx bun.Tx, userIDs []int64) (int64, error) {
	if len(userIDs) == 0 {
		return 0, nil
	}

	var totalAffected int64

	// Delete user reasons
	result, err := tx.NewDelete().
		Model((*types.UserReason)(nil)).
		Where("user_id IN (?)", bun.In(userIDs)).
		Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to delete user reasons: %w", err)
	}

	affected, _ := result.RowsAffected()
	totalAffected += affected

	// Delete users
	result, err = tx.NewDelete().
		Model((*types.User)(nil)).
		Where("id IN (?)", bun.In(userIDs)).
		Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to delete users: %w", err)
	}

	affected, _ = result.RowsAffected()
	totalAffected += affected

	// Delete verifications
	result, err = tx.NewDelete().
		Model((*types.UserVerification)(nil)).
		Where("user_id IN (?)", bun.In(userIDs)).
		Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to delete verifications: %w", err)
	}

	affected, _ = result.RowsAffected()
	totalAffected += affected

	// Delete clearances
	result, err = tx.NewDelete().
		Model((*types.UserClearance)(nil)).
		Where("user_id IN (?)", bun.In(userIDs)).
		Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to delete clearances: %w", err)
	}

	affected, _ = result.RowsAffected()
	totalAffected += affected

	r.logger.Debug("Deleted users and their core data",
		zap.Int("count", len(userIDs)),
		zap.Int64("affectedRows", totalAffected))

	return totalAffected, nil
}

// DeleteUserGroups removes user group relationships and unreferenced group info.
func (r *UserModel) DeleteUserGroups(ctx context.Context, tx bun.Tx, userIDs []int64) (int64, error) {
	if len(userIDs) == 0 {
		return 0, nil
	}

	var totalAffected int64

	// Find group IDs belonging to these users
	var groupIDs []int64

	err := tx.NewSelect().
		Model((*types.UserGroup)(nil)).
		Column("group_id").
		Where("user_id IN (?)", bun.In(userIDs)).
		Group("group_id").
		Scan(ctx, &groupIDs)
	if err != nil {
		return 0, fmt.Errorf("failed to find group IDs for deletion: %w", err)
	}

	// Delete user groups
	result, err := tx.NewDelete().
		Model((*types.UserGroup)(nil)).
		Where("user_id IN (?)", bun.In(userIDs)).
		Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to delete user groups: %w", err)
	}

	affected, _ := result.RowsAffected()
	totalAffected += affected

	// Delete orphaned group info only for affected group IDs
	if len(groupIDs) > 0 {
		result, err = tx.NewRaw(`
			DELETE FROM group_infos
			WHERE id IN (?)
			AND NOT EXISTS (
				SELECT 1 FROM user_groups ug WHERE ug.group_id = group_infos.id
			)
			RETURNING id
		`, bun.In(groupIDs)).Exec(ctx)
		if err != nil {
			return totalAffected, fmt.Errorf("failed to delete unreferenced group info: %w", err)
		}

		affected, _ = result.RowsAffected()
		totalAffected += affected
	}

	return totalAffected, nil
}

// DeleteUserOutfits removes user outfit relationships and unreferenced outfits.
func (r *UserModel) DeleteUserOutfits(ctx context.Context, tx bun.Tx, userIDs []int64) (int64, error) {
	if len(userIDs) == 0 {
		return 0, nil
	}

	var totalAffected int64

	// Find outfit IDs belonging to these users
	var outfitIDs []int64

	err := tx.NewSelect().
		Model((*types.UserOutfit)(nil)).
		Column("outfit_id").
		Where("user_id IN (?)", bun.In(userIDs)).
		Group("outfit_id").
		Scan(ctx, &outfitIDs)
	if err != nil {
		return 0, fmt.Errorf("failed to find outfit IDs for deletion: %w", err)
	}

	// Delete user outfits
	result, err := tx.NewDelete().
		Model((*types.UserOutfit)(nil)).
		Where("user_id IN (?)", bun.In(userIDs)).
		Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to delete user outfits: %w", err)
	}

	affected, _ := result.RowsAffected()
	totalAffected += affected

	// Delete orphaned outfit info only for affected outfit IDs
	if len(outfitIDs) > 0 {
		result, err = tx.NewRaw(`
			DELETE FROM outfit_infos
			WHERE id IN (?)
			AND NOT EXISTS (
				SELECT 1 FROM user_outfits uo WHERE uo.outfit_id = outfit_infos.id
			)
			RETURNING id
		`, bun.In(outfitIDs)).Exec(ctx)
		if err != nil {
			return totalAffected, fmt.Errorf("failed to delete unreferenced outfits: %w", err)
		}

		affected, _ = result.RowsAffected()
		totalAffected += affected
	}

	return totalAffected, nil
}

// DeleteUserFriends removes user friend relationships and unreferenced friend info.
func (r *UserModel) DeleteUserFriends(ctx context.Context, tx bun.Tx, userIDs []int64) (int64, error) {
	if len(userIDs) == 0 {
		return 0, nil
	}

	var totalAffected int64

	// Find friend IDs belonging to these users
	var friendIDs []int64

	err := tx.NewSelect().
		Model((*types.UserFriend)(nil)).
		Column("friend_id").
		Where("user_id IN (?)", bun.In(userIDs)).
		Group("friend_id").
		Scan(ctx, &friendIDs)
	if err != nil {
		return 0, fmt.Errorf("failed to find friend IDs for deletion: %w", err)
	}

	// Delete user friends
	result, err := tx.NewDelete().
		Model((*types.UserFriend)(nil)).
		Where("user_id IN (?)", bun.In(userIDs)).
		Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to delete user friends: %w", err)
	}

	affected, _ := result.RowsAffected()
	totalAffected += affected

	// Delete orphaned friend info only for affected friend IDs
	if len(friendIDs) > 0 {
		result, err = tx.NewRaw(`
			DELETE FROM friend_infos
			WHERE id IN (?)
			AND NOT EXISTS (
				SELECT 1 FROM user_friends uf WHERE uf.friend_id = friend_infos.id
			)
			RETURNING id
		`, bun.In(friendIDs)).Exec(ctx)
		if err != nil {
			return totalAffected, fmt.Errorf("failed to delete unreferenced friend info: %w", err)
		}

		affected, _ = result.RowsAffected()
		totalAffected += affected
	}

	return totalAffected, nil
}

// DeleteUserFavorites removes favorite games for the specified users and their associated info.
func (r *UserModel) DeleteUserFavorites(ctx context.Context, tx bun.Tx, userIDs []int64) (int64, error) {
	if len(userIDs) == 0 {
		return 0, nil
	}

	var totalAffected int64

	// Find favorite game IDs belonging to these users
	var favGameIDs []int64

	err := tx.NewSelect().
		Model((*types.UserFavorite)(nil)).
		Column("game_id").
		Where("user_id IN (?)", bun.In(userIDs)).
		Group("game_id").
		Scan(ctx, &favGameIDs)
	if err != nil {
		return 0, fmt.Errorf("failed to find favorite game IDs for deletion: %w", err)
	}

	// Delete user favorites
	result, err := tx.NewDelete().
		Model((*types.UserFavorite)(nil)).
		Where("user_id IN (?)", bun.In(userIDs)).
		Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to delete user favorites: %w", err)
	}

	affected, _ := result.RowsAffected()
	totalAffected += affected

	// Delete orphaned game info only for affected favorite game IDs
	if len(favGameIDs) > 0 {
		result, err = tx.NewRaw(`
			DELETE FROM game_infos
			WHERE id IN (?)
			AND NOT EXISTS (
				SELECT 1 FROM user_favorites uf WHERE uf.game_id = game_infos.id
			)
			AND NOT EXISTS (
				SELECT 1 FROM user_games ug WHERE ug.game_id = game_infos.id
			)
			RETURNING id
		`, bun.In(favGameIDs)).Exec(ctx)
		if err != nil {
			return totalAffected, fmt.Errorf("failed to delete orphaned game info: %w", err)
		}

		affected, _ = result.RowsAffected()
		totalAffected += affected
	}

	return totalAffected, nil
}

// DeleteUserGames removes user game relationships and unreferenced game info.
func (r *UserModel) DeleteUserGames(ctx context.Context, tx bun.Tx, userIDs []int64) (int64, error) {
	if len(userIDs) == 0 {
		return 0, nil
	}

	var totalAffected int64

	// Find game IDs belonging to these users
	var gameIDs []int64

	err := tx.NewSelect().
		Model((*types.UserGame)(nil)).
		Column("game_id").
		Where("user_id IN (?)", bun.In(userIDs)).
		Group("game_id").
		Scan(ctx, &gameIDs)
	if err != nil {
		return 0, fmt.Errorf("failed to find game IDs for deletion: %w", err)
	}

	// Delete user games
	result, err := tx.NewDelete().
		Model((*types.UserGame)(nil)).
		Where("user_id IN (?)", bun.In(userIDs)).
		Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to delete user games: %w", err)
	}

	affected, _ := result.RowsAffected()
	totalAffected += affected

	// Delete orphaned game info only for affected game IDs
	if len(gameIDs) > 0 {
		result, err = tx.NewRaw(`
			DELETE FROM game_infos
			WHERE id IN (?)
			AND NOT EXISTS (
				SELECT 1 FROM user_games ug WHERE ug.game_id = game_infos.id
			)
			AND NOT EXISTS (
				SELECT 1 FROM user_favorites uf WHERE uf.game_id = game_infos.id
			)
			RETURNING id
		`, bun.In(gameIDs)).Exec(ctx)
		if err != nil {
			return totalAffected, fmt.Errorf("failed to delete orphaned game info: %w", err)
		}

		affected, _ = result.RowsAffected()
		totalAffected += affected
	}

	return totalAffected, nil
}

// DeleteUserInventory removes user inventory relationships and unreferenced inventory info.
func (r *UserModel) DeleteUserInventory(ctx context.Context, tx bun.Tx, userIDs []int64) (int64, error) {
	if len(userIDs) == 0 {
		return 0, nil
	}

	var totalAffected int64

	// Find inventory IDs belonging to these users
	var invIDs []int64

	err := tx.NewSelect().
		Model((*types.UserInventory)(nil)).
		Column("inventory_id").
		Where("user_id IN (?)", bun.In(userIDs)).
		Group("inventory_id").
		Scan(ctx, &invIDs)
	if err != nil {
		return 0, fmt.Errorf("failed to find inventory IDs for deletion: %w", err)
	}

	// Delete user inventories
	result, err := tx.NewDelete().
		Model((*types.UserInventory)(nil)).
		Where("user_id IN (?)", bun.In(userIDs)).
		Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to delete user inventories: %w", err)
	}

	affected, _ := result.RowsAffected()
	totalAffected += affected

	// Delete orphaned inventory info only for affected inventory IDs
	if len(invIDs) > 0 {
		result, err = tx.NewRaw(`
			DELETE FROM inventory_infos
			WHERE id IN (?)
			AND NOT EXISTS (
				SELECT 1 FROM user_inventories ui WHERE ui.inventory_id = inventory_infos.id
			)
			RETURNING id
		`, bun.In(invIDs)).Exec(ctx)
		if err != nil {
			return totalAffected, fmt.Errorf("failed to delete unreferenced inventory items: %w", err)
		}

		affected, _ = result.RowsAffected()
		totalAffected += affected
	}

	return totalAffected, nil
}

// GetUserToScan finds the next confirmed user to scan with specific high-priority categories.
func (r *UserModel) GetUserToScan(ctx context.Context) (*types.User, error) {
	var user types.User

	err := dbretry.Transaction(ctx, r.db, func(ctx context.Context, tx bun.Tx) error {
		// Query confirmed users with specific high-priority categories
		err := tx.NewSelect().Model(&user).
			Where("status = ?", enum.UserTypeConfirmed).
			Where("is_banned = false").
			Where("last_scanned < NOW() - INTERVAL '1 day'").
			Where("category IN (?)", bun.In([]enum.UserCategoryType{
				enum.UserCategoryTypePredatory,
				enum.UserCategoryTypeCSAM,
				enum.UserCategoryTypeSexual,
				enum.UserCategoryTypeRaceplay,
			})).
			Where("(EXISTS (SELECT 1 FROM user_reasons ur1 WHERE ur1.user_id = \"user\".id AND reason_type = ?) AND "+
				"EXISTS (SELECT 1 FROM user_reasons ur2 WHERE ur2.user_id = \"user\".id AND reason_type != ?)) OR "+
				"(EXISTS (SELECT 1 FROM user_reasons ur3 WHERE ur3.user_id = \"user\".id AND reason_type = ?) AND "+
				"NOT EXISTS (SELECT 1 FROM user_reasons ur4 WHERE ur4.user_id = \"user\".id AND reason_type = ?))",
				enum.UserReasonTypeFriend, enum.UserReasonTypeFriend, enum.UserReasonTypeProfile, enum.UserReasonTypeOutfit).
			OrderExpr("last_scanned ASC, confidence DESC").
			Limit(1).
			For("UPDATE SKIP LOCKED").
			Scan(ctx)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("no users available to scan: %w", err)
			}

			return fmt.Errorf("failed to query confirmed users: %w", err)
		}

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
	ctx context.Context, targetStatus enum.UserType, sortBy enum.ReviewSortBy, recentIDs []int64,
) (*types.ReviewUser, error) {
	var (
		user   types.User
		result types.ReviewUser
	)

	err := dbretry.Transaction(ctx, r.db, func(ctx context.Context, tx bun.Tx) error {
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
		case enum.ReviewSortByRecentlyUpdated:
			query.OrderExpr("last_updated DESC, last_viewed ASC")
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

		// Get user reasons
		var reasons []*types.UserReason

		err = tx.NewSelect().
			Model(&reasons).
			Where("user_id = ?", user.ID).
			Scan(ctx)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("failed to get user reasons: %w", err)
		}

		// Convert reasons to map
		result.Reasons = make(types.Reasons[enum.UserReasonType])
		for _, reason := range reasons {
			result.Reasons[reason.ReasonType] = &types.Reason{
				Message:    reason.Message,
				Confidence: reason.Confidence,
				Evidence:   reason.Evidence,
			}
		}

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
			// No additional data to load for flagged users
		case enum.UserTypeQueued, enum.UserTypeBloxDB, enum.UserTypeMixed, enum.UserTypePastOffender:
			// No verification/clearance data for these statuses
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

// GetUsersGroups fetches groups for multiple users.
func (r *UserModel) GetUsersGroups(ctx context.Context, userIDs []int64) (map[int64][]*apiTypes.UserGroupRoles, error) {
	if len(userIDs) == 0 {
		return make(map[int64][]*apiTypes.UserGroupRoles), nil
	}

	var userGroups []*types.UserGroup

	err := dbretry.NoResult(ctx, func(ctx context.Context) error {
		return r.db.NewSelect().
			Model(&userGroups).
			Relation("Group").
			Where("user_group.user_id IN (?)", bun.In(userIDs)).
			Order("user_group.user_id", "user_group.role_rank DESC").
			Scan(ctx)
	})

	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("failed to get users groups: %w", err)
	}

	// Group by user ID
	result := make(map[int64][]*apiTypes.UserGroupRoles)

	for _, userGroup := range userGroups {
		if userGroup.Group == nil {
			continue
		}

		group := userGroup.Group
		isLocked := group.IsLocked

		apiGroup := &apiTypes.UserGroupRoles{
			Group: apiTypes.GroupResponse{
				ID:                 userGroup.GroupID,
				Name:               group.Name,
				Description:        group.Description,
				Owner:              group.Owner,
				Shout:              group.Shout,
				MemberCount:        group.MemberCount,
				HasVerifiedBadge:   group.HasVerifiedBadge,
				IsBuildersClubOnly: group.IsBuildersClubOnly,
				PublicEntryAllowed: group.PublicEntryAllowed,
				IsLocked:           &isLocked,
			},
			Role: apiTypes.UserGroupRole{
				ID:   userGroup.RoleID,
				Name: userGroup.RoleName,
				Rank: userGroup.RoleRank,
			},
		}

		result[userGroup.UserID] = append(result[userGroup.UserID], apiGroup)
	}

	return result, nil
}

// GetUsersOutfits fetches outfits for multiple users.
func (r *UserModel) GetUsersOutfits(ctx context.Context, userIDs []int64) (map[int64]*types.UserOutfitsResult, error) {
	if len(userIDs) == 0 {
		return make(map[int64]*types.UserOutfitsResult), nil
	}

	var userOutfits []*types.UserOutfit

	err := dbretry.NoResult(ctx, func(ctx context.Context) error {
		return r.db.NewSelect().
			Model(&userOutfits).
			Relation("Outfit").
			Relation("Outfit.OutfitAssets").
			Relation("Outfit.OutfitAssets.Asset").
			Where("user_outfit.user_id IN (?)", bun.In(userIDs)).
			Scan(ctx)
	})

	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("failed to get users outfits: %w", err)
	}

	// Group by user ID
	result := make(map[int64]*types.UserOutfitsResult)

	for _, userOutfit := range userOutfits {
		if userOutfit.Outfit == nil {
			continue
		}

		// Initialize user result if not exists
		if _, exists := result[userOutfit.UserID]; !exists {
			result[userOutfit.UserID] = &types.UserOutfitsResult{
				Outfits:      make([]*apiTypes.Outfit, 0),
				OutfitAssets: make(map[int64][]*apiTypes.AssetV2),
			}
		}

		userResult := result[userOutfit.UserID]

		// Convert outfit to API type
		apiOutfit := &apiTypes.Outfit{
			ID:         userOutfit.OutfitID,
			Name:       userOutfit.Outfit.Name,
			IsEditable: userOutfit.Outfit.IsEditable,
			OutfitType: userOutfit.Outfit.OutfitType,
		}
		userResult.Outfits = append(userResult.Outfits, apiOutfit)

		// Process outfit assets if any
		var outfitAssets []*apiTypes.AssetV2

		for _, outfitAsset := range userOutfit.Outfit.OutfitAssets {
			if outfitAsset.Asset == nil {
				continue
			}

			outfitAssets = append(outfitAssets, &apiTypes.AssetV2{
				ID:   outfitAsset.AssetID,
				Name: outfitAsset.Asset.Name,
				AssetType: apiTypes.AssetType{
					ID: outfitAsset.Asset.AssetType,
				},
				CurrentVersionID: outfitAsset.CurrentVersionID,
			})
		}

		if len(outfitAssets) > 0 {
			userResult.OutfitAssets[userOutfit.OutfitID] = outfitAssets
		}
	}

	return result, nil
}

// GetUsersAssets fetches the current assets for multiple users.
func (r *UserModel) GetUsersAssets(ctx context.Context, userIDs []int64) (map[int64][]*apiTypes.AssetV2, error) {
	if len(userIDs) == 0 {
		return make(map[int64][]*apiTypes.AssetV2), nil
	}

	var userAssets []*types.UserAsset

	err := dbretry.NoResult(ctx, func(ctx context.Context) error {
		return r.db.NewSelect().
			Model(&userAssets).
			Relation("Asset").
			Where("user_asset.user_id IN (?)", bun.In(userIDs)).
			Scan(ctx)
	})
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("failed to get users assets: %w", err)
	}

	// Group by user ID
	result := make(map[int64][]*apiTypes.AssetV2)

	for _, userAsset := range userAssets {
		if userAsset.Asset == nil {
			continue
		}

		apiAsset := &apiTypes.AssetV2{
			ID:   userAsset.AssetID,
			Name: userAsset.Asset.Name,
			AssetType: apiTypes.AssetType{
				ID: userAsset.Asset.AssetType,
			},
			CurrentVersionID: userAsset.CurrentVersionID,
		}

		result[userAsset.UserID] = append(result[userAsset.UserID], apiAsset)
	}

	return result, nil
}

// GetUsersFriends fetches friends for multiple users.
func (r *UserModel) GetUsersFriends(ctx context.Context, userIDs []int64) (map[int64][]*apiTypes.ExtendedFriend, error) {
	if len(userIDs) == 0 {
		return make(map[int64][]*apiTypes.ExtendedFriend), nil
	}

	var userFriends []*types.UserFriend

	err := dbretry.NoResult(ctx, func(ctx context.Context) error {
		return r.db.NewSelect().
			Model(&userFriends).
			Relation("Friend").
			Where("user_friend.user_id IN (?)", bun.In(userIDs)).
			Scan(ctx)
	})

	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("failed to get users friends: %w", err)
	}

	// Group by user ID
	result := make(map[int64][]*apiTypes.ExtendedFriend)

	for _, userFriend := range userFriends {
		if userFriend.Friend == nil {
			continue
		}

		apiFriend := &apiTypes.ExtendedFriend{
			Friend: apiTypes.Friend{
				ID: userFriend.FriendID,
			},
			Name:        userFriend.Friend.Name,
			DisplayName: userFriend.Friend.DisplayName,
		}

		result[userFriend.UserID] = append(result[userFriend.UserID], apiFriend)
	}

	return result, nil
}

// GetUsersFavorites fetches favorite games for multiple users.
func (r *UserModel) GetUsersFavorites(ctx context.Context, userIDs []int64) (map[int64][]*apiTypes.Game, error) {
	if len(userIDs) == 0 {
		return make(map[int64][]*apiTypes.Game), nil
	}

	var userFavorites []*types.UserFavorite

	err := dbretry.NoResult(ctx, func(ctx context.Context) error {
		return r.db.NewSelect().
			Model(&userFavorites).
			Relation("Game").
			Where("user_favorite.user_id IN (?)", bun.In(userIDs)).
			Order("user_favorite.user_id", "game.place_visits DESC").
			Scan(ctx)
	})

	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("failed to get users favorites: %w", err)
	}

	// Group by user ID
	result := make(map[int64][]*apiTypes.Game)
	for _, userFavorite := range userFavorites {
		if userFavorite.Game == nil {
			continue
		}

		game := userFavorite.Game
		apiGame := &apiTypes.Game{
			ID:          userFavorite.GameID,
			Name:        game.Name,
			Description: game.Description,
			PlaceVisits: game.PlaceVisits,
			Created:     game.Created,
			Updated:     game.Updated,
		}

		result[userFavorite.UserID] = append(result[userFavorite.UserID], apiGame)
	}

	return result, nil
}

// GetUsersGames fetches games for multiple users.
func (r *UserModel) GetUsersGames(ctx context.Context, userIDs []int64) (map[int64][]*apiTypes.Game, error) {
	if len(userIDs) == 0 {
		return make(map[int64][]*apiTypes.Game), nil
	}

	var userGames []*types.UserGame

	err := dbretry.NoResult(ctx, func(ctx context.Context) error {
		return r.db.NewSelect().
			Model(&userGames).
			Relation("Game").
			Where("user_game.user_id IN (?)", bun.In(userIDs)).
			Order("user_game.user_id", "game.place_visits DESC").
			Scan(ctx)
	})

	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("failed to get users games: %w", err)
	}

	// Group by user ID
	result := make(map[int64][]*apiTypes.Game)
	for _, userGame := range userGames {
		if userGame.Game == nil {
			continue
		}

		game := userGame.Game
		apiGame := &apiTypes.Game{
			ID:          userGame.GameID,
			Name:        game.Name,
			Description: game.Description,
			PlaceVisits: game.PlaceVisits,
			Created:     game.Created,
			Updated:     game.Updated,
		}

		result[userGame.UserID] = append(result[userGame.UserID], apiGame)
	}

	return result, nil
}

// GetUsersInventory fetches inventory for multiple users.
func (r *UserModel) GetUsersInventory(ctx context.Context, userIDs []int64) (map[int64][]*apiTypes.InventoryAsset, error) {
	if len(userIDs) == 0 {
		return make(map[int64][]*apiTypes.InventoryAsset), nil
	}

	var userInventories []*types.UserInventory

	err := dbretry.NoResult(ctx, func(ctx context.Context) error {
		return r.db.NewSelect().
			Model(&userInventories).
			Relation("Inventory").
			Where("user_inventory.user_id IN (?)", bun.In(userIDs)).
			Scan(ctx)
	})

	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("failed to get users inventory: %w", err)
	}

	// Group by user ID
	result := make(map[int64][]*apiTypes.InventoryAsset)

	for _, userInventory := range userInventories {
		if userInventory.Inventory == nil {
			continue
		}

		inventory := userInventory.Inventory
		apiInventory := &apiTypes.InventoryAsset{
			AssetID:   userInventory.InventoryID,
			Name:      inventory.Name,
			AssetType: inventory.AssetType,
			Created:   inventory.Created,
		}

		result[userInventory.UserID] = append(result[userInventory.UserID], apiInventory)
	}

	return result, nil
}

// GetFriendInfos retrieves friend information for a list of friend IDs.
// Returns a map of friend IDs to extended friend objects.
func (r *UserModel) GetFriendInfos(ctx context.Context, friendIDs []int64) (map[int64]*apiTypes.ExtendedFriend, error) {
	if len(friendIDs) == 0 {
		return make(map[int64]*apiTypes.ExtendedFriend), nil
	}

	var friendInfos []*types.FriendInfo

	err := dbretry.NoResult(ctx, func(ctx context.Context) error {
		return r.db.NewSelect().
			Model(&friendInfos).
			Where("id IN (?)", bun.In(friendIDs)).
			Scan(ctx)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get friend info: %w", err)
	}

	// Convert to map of extended friends
	friendMap := make(map[int64]*apiTypes.ExtendedFriend, len(friendInfos))
	for _, friend := range friendInfos {
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
	ctx context.Context, friendIDs []int64, cutoffTime time.Time,
) (map[int64]*apiTypes.ExtendedFriend, error) {
	if len(friendIDs) == 0 {
		return make(map[int64]*apiTypes.ExtendedFriend), nil
	}

	var friendInfos []*types.FriendInfo

	err := dbretry.NoResult(ctx, func(ctx context.Context) error {
		return r.db.NewSelect().
			Model(&friendInfos).
			Where("id IN (?)", bun.In(friendIDs)).
			Where("last_updated > ?", cutoffTime).
			Scan(ctx)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get friend info: %w", err)
	}

	// Convert to map of extended friends
	friendMap := make(map[int64]*apiTypes.ExtendedFriend, len(friendInfos))
	for _, friend := range friendInfos {
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

// SaveUserGroups saves groups for multiple users.
func (r *UserModel) SaveUserGroups(ctx context.Context, tx bun.Tx, userGroups map[int64][]*apiTypes.UserGroupRoles) error {
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
	groupInfoMap := make(map[int64]*types.GroupInfo)

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
	ctx context.Context, tx bun.Tx, userOutfits map[int64][]*apiTypes.Outfit,
	userOutfitAssets map[int64]map[int64][]*apiTypes.AssetV2,
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
	outfitInfoMap := make(map[int64]*types.OutfitInfo)

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
			assetMap = make(map[int64]types.AssetInfo)
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
func (r *UserModel) SaveUserAssets(ctx context.Context, tx bun.Tx, userAssets map[int64][]*apiTypes.AssetV2) error {
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
	assetInfoMap := make(map[int64]*types.AssetInfo)

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
func (r *UserModel) SaveUserFriends(ctx context.Context, tx bun.Tx, userFriends map[int64][]*apiTypes.ExtendedFriend) error {
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
	friendInfoMap := make(map[int64]*types.FriendInfo)

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

// SaveUserFavorites saves favorite games for multiple users.
func (r *UserModel) SaveUserFavorites(ctx context.Context, tx bun.Tx, userFavorites map[int64][]*apiTypes.Game) error {
	if len(userFavorites) == 0 {
		return nil
	}

	// Calculate total size for slices
	totalFavorites := 0
	for _, favorites := range userFavorites {
		totalFavorites += len(favorites)
	}

	// Pre-allocate slices
	allUserFavorites := make([]types.UserFavorite, 0, totalFavorites)
	gameInfoMap := make(map[int64]*types.GameInfo)

	// Build user favorites and game info
	for userID, favorites := range userFavorites {
		for _, game := range favorites {
			userFavorite := &types.UserFavorite{
				UserID: userID,
				GameID: game.ID,
			}
			_, gameInfo := types.FromAPIGame(userID, game)

			allUserFavorites = append(allUserFavorites, *userFavorite)
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

	// Save user favorites
	_, err = tx.NewInsert().
		Model(&allUserFavorites).
		On("CONFLICT (user_id, game_id) DO NOTHING").
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to upsert user favorites: %w", err)
	}

	return nil
}

// SaveUserGames saves games for multiple users.
func (r *UserModel) SaveUserGames(ctx context.Context, tx bun.Tx, userGames map[int64][]*apiTypes.Game) error {
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
	gameInfoMap := make(map[int64]*types.GameInfo)

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
func (r *UserModel) SaveUserInventory(ctx context.Context, tx bun.Tx, userInventory map[int64][]*apiTypes.InventoryAsset) error {
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
	inventoryInfoMap := make(map[int64]*types.InventoryInfo)

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

// GetUsersUpdatedAfter returns users that have been updated after the specified time.
// If reasonTypes is provided, it will filter for users with only those specific reason types.
func (r *UserModel) GetUsersUpdatedAfter(
	ctx context.Context, cutoffTime time.Time, reasonTypes []enum.UserReasonType,
) ([]*types.User, error) {
	var users []*types.User

	err := dbretry.NoResult(ctx, func(ctx context.Context) error {
		query := r.db.NewSelect().
			Model(&users).
			Where("status = ?", enum.UserTypeFlagged).
			Where("last_updated > ?", cutoffTime)

		// Add reason filtering if specified
		if len(reasonTypes) > 0 {
			// Find users that have all the specified reason types
			for _, reasonType := range reasonTypes {
				query = query.Where("id IN (SELECT user_id FROM user_reasons WHERE reason_type = ?)", reasonType)
			}

			// Count of reasons for each user should equal the number of specified reason types
			query = query.Where("id IN (SELECT user_id FROM user_reasons GROUP BY user_id HAVING COUNT(*) = ?)", len(reasonTypes))
		}

		return query.Order("last_updated ASC").Scan(ctx)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get flagged users updated after %s: %w", cutoffTime.Format(time.RFC3339), err)
	}

	r.logger.Debug("Found flagged users updated after cutoff time",
		zap.Time("cutoffTime", cutoffTime),
		zap.Int("count", len(users)))

	return users, nil
}

// UpdateUserReason updates a specific reason for a user.
func (r *UserModel) UpdateUserReason(ctx context.Context, userID int64, reasonType enum.UserReasonType, newMessage string) error {
	err := dbretry.NoResult(ctx, func(ctx context.Context) error {
		_, err := r.db.NewUpdate().
			Model((*types.UserReason)(nil)).
			Set("message = ?", newMessage).
			Where("user_id = ? AND reason_type = ?", userID, reasonType).
			Exec(ctx)

		return err
	})
	if err != nil {
		return fmt.Errorf("failed to update user reason: %w", err)
	}

	r.logger.Debug("Updated user reason",
		zap.Int64("userID", userID),
		zap.String("reasonType", reasonType.String()),
		zap.String("newMessage", newMessage))

	return nil
}

// GetUsersWithoutReason gets users that don't have a specific reason type.
func (r *UserModel) GetUsersWithoutReason(
	ctx context.Context, reasonType enum.UserReasonType, limit int, cursorID int64,
) ([]*types.ReviewUser, error) {
	var users []types.User

	err := dbretry.NoResult(ctx, func(ctx context.Context) error {
		query := r.db.NewSelect().
			Model(&users).
			Column("id", "status").
			Where("status IN (?, ?)", enum.UserTypeFlagged, enum.UserTypeConfirmed).
			Where("id NOT IN (SELECT user_id FROM user_reasons WHERE reason_type = ?)", reasonType).
			Order("id ASC").
			Limit(limit)

		if cursorID > 0 {
			query.Where("id > ?", cursorID)
		}

		return query.Scan(ctx)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get users without reason type %s: %w", reasonType.String(), err)
	}

	// Convert to review users with minimal data
	result := make([]*types.ReviewUser, len(users))
	for i, user := range users {
		result[i] = &types.ReviewUser{
			User: &user,
		}
	}

	r.logger.Debug("Found users without reason type",
		zap.String("reasonType", reasonType.String()),
		zap.Int("count", len(result)),
		zap.Int("limit", limit),
		zap.Int64("cursorID", cursorID))

	return result, nil
}

// GetUsersWithReason gets users that have a specific reason type.
func (r *UserModel) GetUsersWithReason(
	ctx context.Context, reasonType enum.UserReasonType, limit int, cursorID int64,
) ([]*types.ReviewUser, error) {
	var users []types.User

	err := dbretry.NoResult(ctx, func(ctx context.Context) error {
		query := r.db.NewSelect().
			Model(&users).
			Column("id", "status").
			Where("status IN (?, ?)", enum.UserTypeFlagged, enum.UserTypeConfirmed).
			Where("id IN (SELECT user_id FROM user_reasons WHERE reason_type = ?)", reasonType).
			Order("id ASC").
			Limit(limit)

		if cursorID > 0 {
			query.Where("id > ?", cursorID)
		}

		return query.Scan(ctx)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get users with reason type %s: %w", reasonType.String(), err)
	}

	// Convert to review users
	result := make([]*types.ReviewUser, len(users))
	for i, user := range users {
		result[i] = &types.ReviewUser{
			User: &user,
		}
	}

	r.logger.Debug("Found users with reason type",
		zap.String("reasonType", reasonType.String()),
		zap.Int("count", len(result)),
		zap.Int("limit", limit),
		zap.Int64("cursorID", cursorID))

	return result, nil
}

// GetUsersWithoutCategory gets users that don't have a category assigned.
func (r *UserModel) GetUsersWithoutCategory(
	ctx context.Context, limit int, cursorID int64,
) ([]*types.ReviewUser, error) {
	var users []types.User

	err := dbretry.NoResult(ctx, func(ctx context.Context) error {
		query := r.db.NewSelect().
			Model(&users).
			Column("id", "status").
			Where("status IN (?, ?)", enum.UserTypeFlagged, enum.UserTypeConfirmed).
			Where("category IS NULL").
			Order("id ASC").
			Limit(limit)

		if cursorID > 0 {
			query.Where("id > ?", cursorID)
		}

		return query.Scan(ctx)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get users without category: %w", err)
	}

	// Convert to review users with minimal data
	result := make([]*types.ReviewUser, len(users))
	for i, user := range users {
		result[i] = &types.ReviewUser{
			User: &user,
		}
	}

	r.logger.Debug("Found users without category",
		zap.Int("count", len(result)),
		zap.Int("limit", limit),
		zap.Int64("cursorID", cursorID))

	return result, nil
}

// GetUserIDsByCategory returns all user IDs for a specific category.
// Only returns users with flagged or confirmed status (excludes cleared users).
func (r *UserModel) GetUserIDsByCategory(
	ctx context.Context, category enum.UserCategoryType,
) ([]int64, error) {
	var userIDs []int64

	err := dbretry.NoResult(ctx, func(ctx context.Context) error {
		return r.db.NewSelect().
			Model((*types.User)(nil)).
			Column("id").
			Where("status IN (?, ?)", enum.UserTypeFlagged, enum.UserTypeConfirmed).
			Where("category = ?", category).
			Order("id ASC").
			Scan(ctx, &userIDs)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get user IDs by category: %w", err)
	}

	r.logger.Debug("Found users by category",
		zap.Int("count", len(userIDs)),
		zap.Int("category", int(category)))

	return userIDs, nil
}

// UpdateUserCategories updates the category field for multiple users.
func (r *UserModel) UpdateUserCategories(
	ctx context.Context, categories map[int64]enum.UserCategoryType,
) error {
	if len(categories) == 0 {
		return nil
	}

	// Update each user's category
	for userID, category := range categories {
		_, err := r.db.NewUpdate().
			Model((*types.User)(nil)).
			Set("category = ?", category).
			Where("id = ?", userID).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to update category for user %d: %w", userID, err)
		}
	}

	r.logger.Debug("Updated user categories",
		zap.Int("count", len(categories)))

	return nil
}

// GetGlobalTargetCandidates returns candidate users for global targeting in the war system.
func (r *UserModel) GetGlobalTargetCandidates(
	ctx context.Context, limit int, excludeUserIDs []int64,
) ([]*types.GlobalTargetCandidate, error) {
	var candidates []*types.GlobalTargetCandidate

	query := r.db.NewSelect().
		Model((*types.User)(nil)).
		Column("id", "name", "status", "confidence", "category").
		Where("status IN (?, ?)", enum.UserTypeFlagged, enum.UserTypeConfirmed).
		Where("is_banned = ?", false).
		Where(`EXISTS (SELECT 1 FROM user_reasons WHERE user_id = users.id AND reason_type = ?)`, enum.UserReasonTypeProfile).
		Where(`EXISTS (SELECT 1 FROM user_reasons WHERE user_id = users.id AND reason_type = ?)`, enum.UserReasonTypeFriend).
		Where(`EXISTS (SELECT 1 FROM user_reasons WHERE user_id = users.id AND reason_type = ?)`, enum.UserReasonTypeGroup).
		Order("status DESC", "confidence DESC").
		Limit(limit)

	// Exclude recent targets if provided
	if len(excludeUserIDs) > 0 {
		query = query.Where("id NOT IN (?)", bun.In(excludeUserIDs))
	}

	err := dbretry.NoResult(ctx, func(ctx context.Context) error {
		return query.Scan(ctx, &candidates)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get global target candidates: %w", err)
	}

	r.logger.Debug("Retrieved global target candidates",
		zap.Int("count", len(candidates)),
		zap.Int("limit", limit),
		zap.Int("excludeCount", len(excludeUserIDs)))

	return candidates, nil
}

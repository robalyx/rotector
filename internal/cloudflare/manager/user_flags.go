package manager

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/bytedance/sonic"
	"github.com/robalyx/rotector/internal/cloudflare/api"
	"github.com/robalyx/rotector/internal/database"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/database/types/enum"
	"go.uber.org/zap"
)

const (
	// MaxFlaggedUsersBatchSize is the maximum number of flagged users that can be inserted in a single batch.
	MaxFlaggedUsersBatchSize = 10
	// MaxDeleteBatchSize is the maximum number of user IDs that can be deleted in a single batch.
	MaxDeleteBatchSize = 50
	// MaxUpdateBatchSize is the maximum number of records that can be updated in a single batch.
	MaxUpdateBatchSize = 20
)

var ErrInvalidFlagType = errors.New("invalid flag_type for user")

// UserFlags handles user flagging operations.
type UserFlags struct {
	d1         *api.D1Client
	db         database.Client
	warManager *WarManager
	logger     *zap.Logger
}

// NewUserFlags creates a new user flags manager.
func NewUserFlags(d1Client *api.D1Client, db database.Client, warManager *WarManager, logger *zap.Logger) *UserFlags {
	return &UserFlags{
		d1:         d1Client,
		db:         db,
		warManager: warManager,
		logger:     logger,
	}
}

// AddFlagged inserts flagged users into the user_flags table.
func (u *UserFlags) AddFlagged(ctx context.Context, flaggedUsers map[int64]*types.ReviewUser) error {
	return u.addUsers(ctx, flaggedUsers, enum.UserTypeFlagged, nil)
}

// AddConfirmed inserts or updates a confirmed user in the user_flags table.
func (u *UserFlags) AddConfirmed(ctx context.Context, user *types.ReviewUser, reviewerID uint64) error {
	if user == nil {
		return nil
	}

	users := map[int64]*types.ReviewUser{user.ID: user}

	return u.addUsers(ctx, users, enum.UserTypeConfirmed, &reviewerID)
}

// Remove removes a user from the user_flags table.
func (u *UserFlags) Remove(ctx context.Context, userID int64) error {
	// Remove from war system
	if err := u.warManager.RemoveUserFromWarSystem(ctx, userID); err != nil {
		return fmt.Errorf("failed to remove user from war system: %w", err)
	}

	// Remove from user_flags table
	sqlStmt := "DELETE FROM user_flags WHERE user_id = ?"

	_, err := u.d1.ExecuteSQL(ctx, sqlStmt, []any{userID})
	if err != nil {
		return fmt.Errorf("failed to remove user from user_flags: %w", err)
	}

	u.logger.Debug("Removed user from user_flags table",
		zap.Int64("userID", userID))

	return nil
}

// RemoveBatch removes multiple users from the user_flags table in batches.
func (u *UserFlags) RemoveBatch(ctx context.Context, userIDs []int64) error {
	if len(userIDs) == 0 {
		return nil
	}

	// Remove from war system
	if err := u.warManager.RemoveUsersFromWarSystem(ctx, userIDs); err != nil {
		return fmt.Errorf("failed to remove users from war system: %w", err)
	}

	// Process deletions in batches to avoid SQLite variable limits
	for i := 0; i < len(userIDs); i += MaxDeleteBatchSize {
		end := min(i+MaxDeleteBatchSize, len(userIDs))
		batchUserIDs := userIDs[i:end]

		query := "DELETE FROM user_flags WHERE user_id IN ("
		params := make([]any, len(batchUserIDs))

		for j, userID := range batchUserIDs {
			if j > 0 {
				query += ","
			}

			query += "?"
			params[j] = userID
		}

		query += ")"

		_, err := u.d1.ExecuteSQL(ctx, query, params)
		if err != nil {
			return fmt.Errorf("failed to remove users batch %d-%d: %w", i, end-1, err)
		}
	}

	u.logger.Debug("Removed users batch from user_flags table",
		zap.Int("count", len(userIDs)))

	return nil
}

// UpdateBanStatus updates the is_banned field for users in the user_flags table.
func (u *UserFlags) UpdateBanStatus(ctx context.Context, userIDs []int64, isBanned bool) error {
	if len(userIDs) == 0 {
		return nil
	}

	// Build query to update is_banned and last_updated
	query := "UPDATE user_flags SET is_banned = ?, last_updated = ? WHERE user_id IN ("
	params := make([]any, 0, len(userIDs)+2)

	// Add the banned status and timestamp as first parameters
	if isBanned {
		params = append(params, 1)
	} else {
		params = append(params, 0)
	}

	params = append(params, time.Now().Unix())

	// Add placeholders for WHERE clause
	for i, userID := range userIDs {
		if i > 0 {
			query += ","
		}

		query += "?"

		params = append(params, userID)
	}

	query += ")"

	result, err := u.d1.ExecuteSQL(ctx, query, params)
	if err != nil {
		return fmt.Errorf("failed to update user ban status: %w", err)
	}

	u.logger.Debug("Updated user ban status in user_flags table",
		zap.Int("users_processed", len(userIDs)),
		zap.Bool("is_banned", isBanned),
		zap.Int("rows_affected", len(result)))

	return nil
}

// UpdateToPastOffender updates users to past offender status in D1 when they become clean.
func (u *UserFlags) UpdateToPastOffender(ctx context.Context, userIDs []int64) error {
	if len(userIDs) == 0 {
		return nil
	}

	// Process updates in batches to avoid SQLite variable limits
	for i := 0; i < len(userIDs); i += MaxUpdateBatchSize {
		end := min(i+MaxUpdateBatchSize, len(userIDs))
		batchUserIDs := userIDs[i:end]

		query := `UPDATE user_flags
			SET flag_type = ?,
			    is_reportable = 0,
			    last_updated = ?
			WHERE user_id IN (`

		params := make([]any, 0, len(batchUserIDs)+2)
		params = append(params, enum.UserTypePastOffender, time.Now().Unix())

		for j, userID := range batchUserIDs {
			if j > 0 {
				query += ","
			}

			query += "?"

			params = append(params, userID)
		}

		query += ")"

		_, err := u.d1.ExecuteSQL(ctx, query, params)
		if err != nil {
			return fmt.Errorf("failed to update users to past offender status (batch %d-%d): %w", i, end-1, err)
		}
	}

	u.logger.Debug("Updated users to past offender status in D1",
		zap.Int("count", len(userIDs)))

	return nil
}

// addUsers is the unified method for adding users.
func (u *UserFlags) addUsers(
	ctx context.Context, users map[int64]*types.ReviewUser, flagType enum.UserType, reviewerID *uint64,
) error {
	if len(users) == 0 {
		return nil
	}

	// Fetch reviewer information if needed
	var reviewerUsername, reviewerDisplayName string
	if reviewerID != nil {
		reviewerUsername = "Unknown"
		reviewerDisplayName = "Unknown"

		if reviewerInfos, err := u.db.Model().Reviewer().GetReviewerInfos(ctx, []uint64{*reviewerID}); err != nil {
			u.logger.Warn("Failed to get reviewer info",
				zap.Error(err),
				zap.Uint64("reviewerID", *reviewerID))
		} else if reviewerInfo, exists := reviewerInfos[*reviewerID]; exists {
			reviewerUsername = reviewerInfo.Username
			reviewerDisplayName = reviewerInfo.DisplayName
		}
	}

	// Convert map to slice
	userIDs := make([]int64, 0, len(users))
	for userID := range users {
		userIDs = append(userIDs, userID)
	}

	totalProcessed := 0

	// Process users in batches
	for i := 0; i < len(userIDs); i += MaxFlaggedUsersBatchSize {
		end := min(i+MaxFlaggedUsersBatchSize, len(userIDs))
		batchUserIDs := userIDs[i:end]

		// Process each user
		for _, userID := range batchUserIDs {
			user := users[userID]

			// Prepare user record data
			reasonsJSON, err := u.prepareUserRecord(user)
			if err != nil {
				return fmt.Errorf("failed to prepare user record for %d: %w", userID, err)
			}

			// Insert the user record
			sqlStmt := `
				INSERT OR REPLACE INTO user_flags (
					user_id,
					flag_type,
					confidence,
					reasons,
					reviewer_id,
					reviewer_username,
					reviewer_display_name,
					engine_version,
					is_banned,
					is_reportable,
					category,
					last_updated
				) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

			var params []any

			currentTime := time.Now().Unix()

			isReportable := 0
			if user.Reasons[enum.UserReasonTypeProfile] != nil {
				isReportable = 1
			}

			if reviewerID != nil {
				params = []any{
					userID,
					flagType,
					user.Confidence,
					reasonsJSON,
					*reviewerID,
					reviewerUsername,
					reviewerDisplayName,
					user.EngineVersion,
					user.IsBanned,
					isReportable,
					int(user.Category),
					currentTime,
				}
			} else {
				params = []any{
					userID,
					flagType,
					user.Confidence,
					reasonsJSON,
					nil,
					nil,
					nil,
					user.EngineVersion,
					user.IsBanned,
					isReportable,
					int(user.Category),
					currentTime,
				}
			}

			_, err = u.d1.ExecuteSQL(ctx, sqlStmt, params)
			if err != nil {
				return fmt.Errorf("failed to insert user %d: %w", userID, err)
			}
		}

		totalProcessed += len(batchUserIDs)

		logFields := []zap.Field{
			zap.Int("batch_start", i),
			zap.Int("batch_end", end-1),
			zap.Int("batch_count", len(batchUserIDs)),
		}

		if reviewerID != nil {
			logFields = append(logFields, zap.Uint64("reviewerID", *reviewerID))
		}

		u.logger.Debug("Inserted users batch", logFields...)
	}

	logFields := []zap.Field{
		zap.Int("total_count", totalProcessed),
		zap.String("flag_type", flagType.String()),
	}

	if reviewerID != nil {
		logFields = append(logFields, zap.Uint64("reviewerID", *reviewerID))
	}

	u.logger.Debug("Finished inserting users", logFields...)

	return nil
}

// prepareUserRecord prepares the reasons JSON for a user record.
func (u *UserFlags) prepareUserRecord(user *types.ReviewUser) (string, error) {
	reasonsJSON := "{}"

	if len(user.Reasons) > 0 {
		// Convert reasons map to the proper format
		reasonsData := make(map[string]map[string]any)
		for reasonType, reason := range user.Reasons {
			reasonsData[strconv.Itoa(int(reasonType))] = map[string]any{
				"message":    reason.Message,
				"confidence": reason.Confidence,
				"evidence":   reason.Evidence,
			}
		}

		jsonBytes, err := sonic.Marshal(reasonsData)
		if err != nil {
			return "", fmt.Errorf("failed to marshal user reasons: %w", err)
		}

		reasonsJSON = string(jsonBytes)
	}

	return reasonsJSON, nil
}

package manager

import (
	"context"
	"fmt"
	"strconv"

	"github.com/bytedance/sonic"
	"github.com/robalyx/rotector/internal/cloudflare/api"
	"github.com/robalyx/rotector/internal/database"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/database/types/enum"
	"go.uber.org/zap"
)

// UserFlags handles user flagging operations.
type UserFlags struct {
	api    *api.Cloudflare
	db     database.Client
	logger *zap.Logger
}

// NewUserFlags creates a new user flags manager.
func NewUserFlags(cloudflareAPI *api.Cloudflare, db database.Client, logger *zap.Logger) *UserFlags {
	return &UserFlags{
		api:    cloudflareAPI,
		db:     db,
		logger: logger,
	}
}

// AddFlagged inserts flagged users into the user_flags table with integration conflict handling.
func (u *UserFlags) AddFlagged(ctx context.Context, flaggedUsers map[int64]*types.ReviewUser) error {
	return u.addUsersWithMerge(ctx, flaggedUsers, enum.UserTypeFlagged, nil)
}

// AddConfirmed inserts or updates a confirmed user in the user_flags table with integration conflict handling.
func (u *UserFlags) AddConfirmed(ctx context.Context, user *types.ReviewUser, reviewerID uint64) error {
	if user == nil {
		return nil
	}

	users := map[int64]*types.ReviewUser{user.ID: user}

	return u.addUsersWithMerge(ctx, users, enum.UserTypeConfirmed, &reviewerID)
}

// Remove removes a user from the user_flags table.
func (u *UserFlags) Remove(ctx context.Context, userID int64) error {
	sqlStmt := "DELETE FROM user_flags WHERE user_id = ?"

	_, err := u.api.ExecuteSQL(ctx, sqlStmt, []any{userID})
	if err != nil {
		return fmt.Errorf("failed to remove user from user_flags: %w", err)
	}

	u.logger.Debug("Removed user from user_flags table",
		zap.Int64("userID", userID))

	return nil
}

// UpdateBanStatus updates the is_banned field for users in the user_flags table.
func (u *UserFlags) UpdateBanStatus(ctx context.Context, userIDs []int64, isBanned bool) error {
	if len(userIDs) == 0 {
		return nil
	}

	// Build query with CASE statement to update is_banned based on user_id
	query := "UPDATE user_flags SET is_banned = ? WHERE user_id IN ("
	params := make([]any, 0, len(userIDs)+1)

	// Add the banned status as first parameter
	if isBanned {
		params = append(params, 1)
	} else {
		params = append(params, 0)
	}

	// Add placeholders for WHERE clause
	for i, userID := range userIDs {
		if i > 0 {
			query += ","
		}

		query += "?"

		params = append(params, userID)
	}

	query += ")"

	result, err := u.api.ExecuteSQL(ctx, query, params)
	if err != nil {
		return fmt.Errorf("failed to update user ban status: %w", err)
	}

	u.logger.Debug("Updated user ban status in user_flags table",
		zap.Int("users_processed", len(userIDs)),
		zap.Bool("is_banned", isBanned),
		zap.Int("rows_affected", len(result)))

	return nil
}

// addUsersWithMerge is the unified method for adding users with integration conflict handling.
func (u *UserFlags) addUsersWithMerge(
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

		// Check for existing integration records
		existingIntegrationRecords, err := u.checkExistingIntegrationRecords(ctx, batchUserIDs)
		if err != nil {
			return fmt.Errorf("failed to check existing integration records: %w", err)
		}

		// Process each user for merging
		for _, userID := range batchUserIDs {
			user := users[userID]

			// Prepare user record data
			existingRecord := existingIntegrationRecords[userID]

			reasonsJSON, integrationSources, err := u.prepareUserRecord(user, existingRecord)
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
					integration_sources
				) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`

			var params []any
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
					integrationSources,
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
					integrationSources,
				}
			}

			_, err = u.api.ExecuteSQL(ctx, sqlStmt, params)
			if err != nil {
				return fmt.Errorf("failed to insert user %d: %w", userID, err)
			}
		}

		totalProcessed += len(batchUserIDs)

		logFields := []zap.Field{
			zap.Int("batch_start", i),
			zap.Int("batch_end", end-1),
			zap.Int("batch_count", len(batchUserIDs)),
			zap.Int("integration_conflicts", len(existingIntegrationRecords)),
		}

		if reviewerID != nil {
			logFields = append(logFields, zap.Uint64("reviewerID", *reviewerID))
		}

		u.logger.Debug("Inserted users batch with merge handling", logFields...)
	}

	logFields := []zap.Field{
		zap.Int("total_count", totalProcessed),
		zap.String("flag_type", flagType.String()),
	}

	if reviewerID != nil {
		logFields = append(logFields, zap.Uint64("reviewerID", *reviewerID))
	}

	u.logger.Debug("Finished inserting users with merge handling", logFields...)

	return nil
}

// checkExistingIntegrationRecords fetches existing integration records for the given user IDs.
func (u *UserFlags) checkExistingIntegrationRecords(ctx context.Context, userIDs []int64) (map[int64]map[string]any, error) {
	if len(userIDs) == 0 {
		return make(map[int64]map[string]any), nil
	}

	// Query for existing integration records
	checkQuery := `
		SELECT user_id, reasons, integration_sources 
		FROM user_flags 
		WHERE user_id IN (`

	checkParams := make([]any, len(userIDs)+1)
	for j, userID := range userIDs {
		if j > 0 {
			checkQuery += ","
		}

		checkQuery += "?"
		checkParams[j] = userID
	}

	checkQuery += ") AND flag_type = ?"
	checkParams[len(userIDs)] = 4 // Integration flag type

	existingRecords, err := u.api.ExecuteSQL(ctx, checkQuery, checkParams)
	if err != nil {
		return nil, fmt.Errorf("failed to check existing integration records: %w", err)
	}

	// Create map of existing integration records
	existingIntegrationRecords := make(map[int64]map[string]any)

	for _, record := range existingRecords {
		if userID, ok := record["user_id"].(float64); ok {
			existingIntegrationRecords[int64(userID)] = record
		}
	}

	return existingIntegrationRecords, nil
}

// prepareUserRecord prepares the reasons JSON and integration sources for a user record.
func (u *UserFlags) prepareUserRecord(user *types.ReviewUser, existingRecord map[string]any) (string, any, error) {
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
			return "", nil, fmt.Errorf("failed to marshal user reasons: %w", err)
		}

		reasonsJSON = string(jsonBytes)
	}

	var (
		finalReasonsJSON   string
		integrationSources any
	)

	// Check if this user has an existing integration record
	if existingRecord != nil {
		// Merge reasons
		existingReasonsJSON, _ := existingRecord["reasons"].(string)
		if existingReasonsJSON == "" {
			existingReasonsJSON = "{}"
		}

		var err error

		finalReasonsJSON, err = u.mergeReasons(existingReasonsJSON, reasonsJSON)
		if err != nil {
			return "", nil, fmt.Errorf("failed to merge reasons: %w", err)
		}

		// Preserve integration sources
		integrationSources = existingRecord["integration_sources"]
	} else {
		finalReasonsJSON = reasonsJSON
	}

	return finalReasonsJSON, integrationSources, nil
}

// mergeReasons merges existing integration reasons with new system reasons.
func (u *UserFlags) mergeReasons(existingReasonsJSON, newReasonsJSON string) (string, error) {
	// Parse existing reasons
	var existingReasons map[string]map[string]any
	if err := sonic.Unmarshal([]byte(existingReasonsJSON), &existingReasons); err != nil {
		return "", fmt.Errorf("failed to parse existing reasons JSON: %w", err)
	}

	// Parse new reasons
	var newReasons map[string]map[string]any
	if err := sonic.Unmarshal([]byte(newReasonsJSON), &newReasons); err != nil {
		return "", fmt.Errorf("failed to parse new reasons JSON: %w", err)
	}

	// Merge reasons - new reasons take precedence for system flags (numeric keys)
	mergedReasons := make(map[string]map[string]any)

	// First copy existing reasons
	for key, value := range existingReasons {
		mergedReasons[key] = value
	}

	// Then add/override with new reasons
	for key, value := range newReasons {
		mergedReasons[key] = value
	}

	// Marshal back to JSON
	mergedJSON, err := sonic.Marshal(mergedReasons)
	if err != nil {
		return "", fmt.Errorf("failed to marshal merged reasons: %w", err)
	}

	return string(mergedJSON), nil
}

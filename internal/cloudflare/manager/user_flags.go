package manager

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/bytedance/sonic"
	"github.com/robalyx/rotector/internal/cloudflare/api"
	"github.com/robalyx/rotector/internal/database"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/database/types/enum"
	"go.uber.org/zap"
)

const (
	// IntegrationFlagType is the flag type used for integration sources (like BloxDB, custom uploads).
	IntegrationFlagType = 4
)

const (
	// MaxFlaggedUsersBatchSize is the maximum number of flagged users that can be inserted in a single batch.
	MaxFlaggedUsersBatchSize = 10
	// MaxDeleteBatchSize is the maximum number of user IDs that can be deleted in a single batch.
	MaxDeleteBatchSize = 50
	// MaxUpdateBatchSize is the maximum number of records that can be updated in a single batch.
	MaxUpdateBatchSize = 20
)

var (
	ErrNoUploadSourceFound = errors.New("no upload source found for integration type")
	ErrInvalidTotalUploads = errors.New("invalid total_uploads value for integration type")
	ErrInvalidFlagType     = errors.New("invalid flag_type for user")
)

// IntegrationUser represents user data for integration sources.
type IntegrationUser struct {
	ID         int64
	Confidence float64
	Message    string
	Evidence   []string
}

// UserFlags handles user flagging operations.
type UserFlags struct {
	d1     *api.D1Client
	db     database.Client
	logger *zap.Logger
}

// NewUserFlags creates a new user flags manager.
func NewUserFlags(d1Client *api.D1Client, db database.Client, logger *zap.Logger) *UserFlags {
	return &UserFlags{
		d1:     d1Client,
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

// AddIntegration processes integration data from upload sources with cleanup and conflict resolution.
func (u *UserFlags) AddIntegration(ctx context.Context, users map[int64]*IntegrationUser, integrationType string) error {
	if len(users) == 0 {
		return nil
	}

	// Step 1: Cleanup existing integration data for this source
	if err := u.cleanupIntegrationData(ctx, integrationType); err != nil {
		return fmt.Errorf("failed to cleanup existing integration data for %s: %w", integrationType, err)
	}

	// Step 2: Get version from upload_sources table
	version, err := u.getIntegrationVersion(ctx, integrationType)
	if err != nil {
		return fmt.Errorf("failed to get integration version for %s: %w", integrationType, err)
	}

	// Step 3: Process new integration data
	return u.processIntegrationUsers(ctx, users, integrationType, version)
}

// cleanupIntegrationData removes the specified integration source from existing records.
func (u *UserFlags) cleanupIntegrationData(ctx context.Context, integrationType string) error {
	// Find all records containing this integration source
	query := `
		SELECT user_id, flag_type, reasons, integration_sources 
		FROM user_flags 
		WHERE integration_sources LIKE ?
	`

	likePattern := fmt.Sprintf("%%\"%s\"%%", integrationType)

	result, err := u.d1.ExecuteSQL(ctx, query, []any{likePattern})
	if err != nil {
		return fmt.Errorf("failed to query existing integration records: %w", err)
	}

	if len(result) == 0 {
		return nil
	}

	var (
		recordsToUpdate = make([]map[string]any, 0, len(result))
		recordsToDelete = make([]int64, 0, len(result))
	)

	for _, record := range result {
		userID, ok := record["user_id"].(float64)
		if !ok {
			continue
		}

		flagType, ok := record["flag_type"].(float64)
		if !ok {
			continue
		}

		integrationSources, _ := record["integration_sources"].(string)
		reasons, _ := record["reasons"].(string)

		// Clean integration_sources
		cleanedSources, isEmpty, err := u.parseAndRemoveJSONKey(integrationSources, integrationType)
		if err != nil {
			return fmt.Errorf("failed to parse integration_sources JSON for user %d: %w", int64(userID), err)
		}

		// Clean reasons to remove integration entries
		cleanedReasons, _, err := u.parseAndRemoveJSONKey(reasons, integrationType)
		if err != nil {
			return fmt.Errorf("failed to parse reasons JSON for user %d: %w", int64(userID), err)
		}

		// Delete integration records that have no remaining sources
		if int(flagType) == IntegrationFlagType && isEmpty {
			recordsToDelete = append(recordsToDelete, int64(userID))
			continue
		}

		// Mark for update
		recordsToUpdate = append(recordsToUpdate, map[string]any{
			"user_id":             int64(userID),
			"integration_sources": cleanedSources,
			"reasons":             cleanedReasons,
		})
	}

	// Delete orphaned integration records
	if len(recordsToDelete) > 0 {
		if err := u.deleteRecords(ctx, recordsToDelete); err != nil {
			return fmt.Errorf("failed to delete orphaned records: %w", err)
		}
	}

	// Update cleaned records
	if len(recordsToUpdate) > 0 {
		if err := u.updateCleanedRecords(ctx, recordsToUpdate); err != nil {
			return fmt.Errorf("failed to update cleaned records: %w", err)
		}
	}

	u.logger.Debug("Cleaned up integration data",
		zap.String("integrationType", integrationType),
		zap.Int("recordsUpdated", len(recordsToUpdate)),
		zap.Int("recordsDeleted", len(recordsToDelete)))

	return nil
}

// getIntegrationVersion retrieves the version string for an integration type.
func (u *UserFlags) getIntegrationVersion(ctx context.Context, integrationType string) (string, error) {
	query := "SELECT total_uploads FROM upload_sources WHERE source_name = ?"

	result, err := u.d1.ExecuteSQL(ctx, query, []any{integrationType})
	if err != nil {
		return "", fmt.Errorf("failed to query upload_sources: %w", err)
	}

	if len(result) == 0 {
		return "", fmt.Errorf("%w: %s", ErrNoUploadSourceFound, integrationType)
	}

	totalUploads, ok := result[0]["total_uploads"].(float64)
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrInvalidTotalUploads, integrationType)
	}

	return fmt.Sprintf("v%d", int(totalUploads)), nil
}

// parseAndRemoveJSONKey parses JSON string and removes a key, returns if the result is empty.
func (u *UserFlags) parseAndRemoveJSONKey(jsonStr, key string) (*string, bool, error) {
	if jsonStr == "" {
		jsonStr = "{}"
	}

	var data map[string]any
	if err := sonic.Unmarshal([]byte(jsonStr), &data); err != nil {
		return nil, false, fmt.Errorf("failed to parse JSON: %w", err)
	}

	// Remove the key
	delete(data, key)

	// Check if empty
	isEmpty := len(data) == 0

	// Return nil for empty JSON objects
	if isEmpty {
		return nil, isEmpty, nil
	}

	// Marshal back to JSON
	result, err := sonic.Marshal(data)
	if err != nil {
		return nil, false, fmt.Errorf("failed to marshal cleaned JSON: %w", err)
	}

	resultStr := string(result)

	return &resultStr, isEmpty, nil
}

// processIntegrationUsers processes new integration data with conflict resolution.
func (u *UserFlags) processIntegrationUsers(ctx context.Context, users map[int64]*IntegrationUser, integrationType, version string) error {
	// Convert map to slice for batch processing
	userIDs := make([]int64, 0, len(users))
	for userID := range users {
		userIDs = append(userIDs, userID)
	}

	totalProcessed := 0

	// Process users in batches
	for i := 0; i < len(userIDs); i += MaxFlaggedUsersBatchSize {
		end := min(i+MaxFlaggedUsersBatchSize, len(userIDs))
		batchUserIDs := userIDs[i:end]

		// Check for existing records in this batch
		existingRecords, err := u.getExistingRecords(ctx, batchUserIDs)
		if err != nil {
			return fmt.Errorf("failed to check existing records: %w", err)
		}

		// Process each user in the batch
		for _, userID := range batchUserIDs {
			user := users[userID]
			existingRecord := existingRecords[userID]

			if err := u.processIntegrationUser(ctx, user, existingRecord, integrationType, version); err != nil {
				u.logger.Error("Failed to process integration user",
					zap.Error(err),
					zap.Int64("userID", userID),
					zap.String("integrationType", integrationType))

				continue
			}
		}

		totalProcessed += len(batchUserIDs)

		u.logger.Debug("Processed integration users batch",
			zap.Int("batchStart", i),
			zap.Int("batchEnd", end-1),
			zap.Int("batchCount", len(batchUserIDs)),
			zap.String("integrationType", integrationType))
	}

	u.logger.Info("Finished processing integration users",
		zap.Int("totalCount", totalProcessed),
		zap.String("integrationType", integrationType),
		zap.String("version", version))

	return nil
}

// getExistingRecords retrieves existing records for the given user IDs.
func (u *UserFlags) getExistingRecords(ctx context.Context, userIDs []int64) (map[int64]map[string]any, error) {
	if len(userIDs) == 0 {
		return make(map[int64]map[string]any), nil
	}

	query := `
		SELECT user_id, flag_type, confidence, reasons, integration_sources
		FROM user_flags 
		WHERE user_id IN (`

	params := make([]any, len(userIDs))
	for j, userID := range userIDs {
		if j > 0 {
			query += ","
		}

		query += "?"
		params[j] = userID
	}

	query += ")"

	result, err := u.d1.ExecuteSQL(ctx, query, params)
	if err != nil {
		return nil, fmt.Errorf("failed to query existing records: %w", err)
	}

	existingRecords := make(map[int64]map[string]any)

	for _, record := range result {
		if userID, ok := record["user_id"].(float64); ok {
			existingRecords[int64(userID)] = record
		}
	}

	return existingRecords, nil
}

// processIntegrationUser processes a single user with conflict resolution.
func (u *UserFlags) processIntegrationUser(
	ctx context.Context, user *IntegrationUser, existingRecord map[string]any, integrationType, version string,
) error {
	// Use the message and evidence directly from the integration user
	message := user.Message
	if message == "" {
		message = "Integration flag"
	}

	evidence := user.Evidence

	newReason := map[string]any{
		"message":    message,
		"confidence": user.Confidence,
	}

	if len(evidence) > 0 {
		newReason["evidence"] = evidence
	}

	// Convert to JSON string for the integration type
	newReasonData := map[string]map[string]any{
		integrationType: newReason,
	}

	newReasonJSON, err := sonic.Marshal(newReasonData)
	if err != nil {
		return fmt.Errorf("failed to marshal new reason: %w", err)
	}

	// Prepare integration sources
	newSourceData := map[string]string{
		integrationType: version,
	}

	newSourceJSON, err := sonic.Marshal(newSourceData)
	if err != nil {
		return fmt.Errorf("failed to marshal integration sources: %w", err)
	}

	// New user to create integration record
	if existingRecord == nil {
		return u.createIntegrationRecord(ctx, user.ID, user.Confidence, string(newReasonJSON), string(newSourceJSON))
	}

	// Existing user to apply conflict resolution
	flagType, ok := existingRecord["flag_type"].(float64)
	if !ok {
		return fmt.Errorf("%w %d", ErrInvalidFlagType, user.ID)
	}

	// Flagged/confirmed user to preserve PostgreSQL authority, merge data
	if int(flagType) == 1 || int(flagType) == 2 {
		return u.mergeWithAuthorityRecord(ctx, user.ID, existingRecord, string(newReasonJSON), string(newSourceJSON))
	}

	// Integration record to update with new data
	return u.updateIntegrationRecord(
		ctx, user.ID, user.Confidence, string(newReasonJSON), string(newSourceJSON), existingRecord,
	)
}

// createIntegrationRecord creates a new integration record for integration data.
func (u *UserFlags) createIntegrationRecord(ctx context.Context, userID int64, confidence float64, reasonsJSON, sourcesJSON string) error {
	query := `
		INSERT OR REPLACE INTO user_flags (
			user_id, 
			flag_type, 
			confidence, 
			reasons,
			integration_sources,
			is_banned
		) VALUES (?, ?, ?, ?, ?, COALESCE((SELECT is_banned FROM user_flags WHERE user_id = ?), 0))`

	_, err := u.d1.ExecuteSQL(ctx, query, []any{userID, IntegrationFlagType, confidence, reasonsJSON, sourcesJSON, userID})
	if err != nil {
		return fmt.Errorf("failed to create integration record for user %d: %w", userID, err)
	}

	return nil
}

// mergeWithAuthorityRecord merges integration data with existing flagged/confirmed records.
func (u *UserFlags) mergeWithAuthorityRecord(
	ctx context.Context, userID int64, existingRecord map[string]any, newReasonJSON, newSourceJSON string,
) error {
	// Merge reasons
	existingReasons := "{}"

	if reasons := existingRecord["reasons"]; reasons != nil {
		if reasonsStr, ok := reasons.(string); ok && reasonsStr != "" {
			existingReasons = reasonsStr
		}
	}

	mergedReasons, err := u.mergeReasons(existingReasons, newReasonJSON)
	if err != nil {
		return fmt.Errorf("failed to merge reasons: %w", err)
	}

	// Merge integration sources
	existingSources := "{}"

	if sources := existingRecord["integration_sources"]; sources != nil {
		if sourcesStr, ok := sources.(string); ok && sourcesStr != "" {
			existingSources = sourcesStr
		}
	}

	mergedSources, err := u.mergeIntegrationSources(existingSources, newSourceJSON)
	if err != nil {
		return fmt.Errorf("failed to merge integration sources: %w", err)
	}

	query := `
		UPDATE user_flags 
		SET reasons = ?, integration_sources = ?
		WHERE user_id = ?`

	_, err = u.d1.ExecuteSQL(ctx, query, []any{mergedReasons, mergedSources, userID})
	if err != nil {
		return fmt.Errorf("failed to update authority record for user %d: %w", userID, err)
	}

	return nil
}

// updateIntegrationRecord updates existing integration records with new data.
func (u *UserFlags) updateIntegrationRecord(
	ctx context.Context, userID int64, confidence float64, newReasonJSON, newSourceJSON string, existingRecord map[string]any,
) error {
	// For integration records, merge with existing data
	existingReasons := "{}"

	if reasons := existingRecord["reasons"]; reasons != nil {
		if reasonsStr, ok := reasons.(string); ok && reasonsStr != "" {
			existingReasons = reasonsStr
		}
	}

	existingSources := "{}"

	if sources := existingRecord["integration_sources"]; sources != nil {
		if sourcesStr, ok := sources.(string); ok && sourcesStr != "" {
			existingSources = sourcesStr
		}
	}

	// Merge reasons and sources
	mergedReasons, err := u.mergeReasons(existingReasons, newReasonJSON)
	if err != nil {
		return fmt.Errorf("failed to merge integration reasons: %w", err)
	}

	mergedSources, err := u.mergeIntegrationSources(existingSources, newSourceJSON)
	if err != nil {
		return fmt.Errorf("failed to merge integration sources: %w", err)
	}

	query := `
		UPDATE user_flags 
		SET confidence = ?, reasons = ?, integration_sources = ?
		WHERE user_id = ?`

	_, err = u.d1.ExecuteSQL(ctx, query, []any{confidence, mergedReasons, mergedSources, userID})
	if err != nil {
		return fmt.Errorf("failed to update integration record for user %d: %w", userID, err)
	}

	return nil
}

// mergeIntegrationSources merges integration sources JSON objects.
func (u *UserFlags) mergeIntegrationSources(existingJSON, newJSON string) (string, error) {
	var existing map[string]string
	if err := sonic.Unmarshal([]byte(existingJSON), &existing); err != nil {
		return "", fmt.Errorf("failed to parse existing integration sources: %w", err)
	}

	var newSources map[string]string
	if err := sonic.Unmarshal([]byte(newJSON), &newSources); err != nil {
		return "", fmt.Errorf("failed to parse new integration sources: %w", err)
	}

	// Merge where new sources take precedence
	for key, value := range newSources {
		existing[key] = value
	}

	result, err := sonic.Marshal(existing)
	if err != nil {
		return "", fmt.Errorf("failed to marshal merged integration sources: %w", err)
	}

	return string(result), nil
}

// deleteRecords deletes records by user IDs in batches to avoid SQLite variable limits.
func (u *UserFlags) deleteRecords(ctx context.Context, userIDs []int64) error {
	if len(userIDs) == 0 {
		return nil
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
			return fmt.Errorf("failed to delete records batch %d-%d: %w", i, end-1, err)
		}
	}

	return nil
}

// updateCleanedRecords updates records with cleaned JSON data.
func (u *UserFlags) updateCleanedRecords(ctx context.Context, records []map[string]any) error {
	if len(records) == 0 {
		return nil
	}

	// Process records in batches to avoid query size limits
	for i := 0; i < len(records); i += MaxUpdateBatchSize {
		end := min(i+MaxUpdateBatchSize, len(records))
		batch := records[i:end]

		if err := u.updateBatch(ctx, batch); err != nil {
			return fmt.Errorf("failed to update batch %d-%d: %w", i, end-1, err)
		}
	}

	return nil
}

// updateBatch performs a single batch update using CASE statements.
func (u *UserFlags) updateBatch(ctx context.Context, records []map[string]any) error {
	if len(records) == 0 {
		return nil
	}

	const whenThen = `WHEN ? THEN ? `

	// Build the batch UPDATE query with CASE statements
	query := `UPDATE user_flags SET `

	// Build integration_sources CASE statement
	query += `integration_sources = CASE user_id `
	params := make([]any, 0, len(records)*4)
	userIDs := make([]int64, 0, len(records))

	for _, record := range records {
		userID := record["user_id"].(int64)
		integrationSources := record["integration_sources"].(*string)

		query += whenThen

		params = append(params, userID)

		if integrationSources == nil {
			params = append(params, nil)
		} else {
			params = append(params, *integrationSources)
		}

		userIDs = append(userIDs, userID)
	}

	query += `END, `

	// Build reasons CASE statement
	query += `reasons = CASE user_id `

	for _, record := range records {
		userID := record["user_id"].(int64)
		reasons := record["reasons"].(*string)

		query += whenThen

		params = append(params, userID)

		if reasons == nil {
			params = append(params, nil)
		} else {
			params = append(params, *reasons)
		}
	}

	query += `END `

	// Add WHERE clause to limit updates to only the records we're changing
	query += `WHERE user_id IN (`

	for i, userID := range userIDs {
		if i > 0 {
			query += `, `
		}

		query += `?`

		params = append(params, userID)
	}

	query += `)`

	_, err := u.d1.ExecuteSQL(ctx, query, params)
	if err != nil {
		return fmt.Errorf("failed to execute batch update: %w", err)
	}

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
					integration_sources,
					is_banned
				) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

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
					user.IsBanned,
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
					user.IsBanned,
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
	checkParams[len(userIDs)] = IntegrationFlagType

	existingRecords, err := u.d1.ExecuteSQL(ctx, checkQuery, checkParams)
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
		existingReasonsJSON := "{}"

		if reasons := existingRecord["reasons"]; reasons != nil {
			if reasonsStr, ok := reasons.(string); ok && reasonsStr != "" {
				existingReasonsJSON = reasonsStr
			}
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

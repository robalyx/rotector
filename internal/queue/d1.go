package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/robalyx/rotector/internal/database"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/database/types/enum"
	"github.com/robalyx/rotector/internal/setup/config"
	"go.uber.org/zap"
)

const (
	// MaxQueueBatchSize is the maximum number of users that can be queued in a single batch.
	MaxQueueBatchSize = 50
	// MaxFlaggedUsersBatchSize is the maximum number of flagged users that can be inserted in a single batch.
	MaxFlaggedUsersBatchSize = 10
)

// Stats represents queue statistics.
type Stats struct {
	TotalItems  int
	Processing  int
	Unprocessed int
}

// Status represents the current status of a queued user.
type Status struct {
	Processing          bool
	Processed           bool
	Flagged             bool
	InappropriateOutfit bool
}

// UserBatch represents a batch of users with their inappropriate flags.
type UserBatch struct {
	UserIDs                   []uint64
	InappropriateOutfitFlags  map[uint64]struct{}
	InappropriateProfileFlags map[uint64]struct{}
	InappropriateFriendsFlags map[uint64]struct{}
	InappropriateGroupsFlags  map[uint64]struct{}
}

// D1Client handles Cloudflare D1 API requests.
type D1Client struct {
	api    *CloudflareAPI
	db     database.Client
	logger *zap.Logger
}

// NewD1Client creates a new D1 client.
func NewD1Client(cfg *config.Config, db database.Client, logger *zap.Logger) *D1Client {
	api := NewCloudflareAPI(
		cfg.Worker.Cloudflare.AccountID,
		cfg.Worker.Cloudflare.DatabaseID,
		cfg.Worker.Cloudflare.APIToken,
		cfg.Worker.Cloudflare.APIEndpoint,
	)

	return &D1Client{
		api:    api,
		db:     db,
		logger: logger.Named("d1_client"),
	}
}

// GetNextBatch retrieves the next batch of unprocessed and non-processing users.
func (c *D1Client) GetNextBatch(ctx context.Context, batchSize int) (*UserBatch, error) {
	// First, get the batch of users
	selectQuery := `
		SELECT user_id, inappropriate_outfit, inappropriate_profile, inappropriate_friends, inappropriate_groups 
		FROM queued_users 
		WHERE processed = 0 AND processing = 0
		ORDER BY queued_at ASC 
		LIMIT ?
	`

	result, err := c.api.ExecuteSQL(ctx, selectQuery, []any{batchSize})
	if err != nil {
		return nil, fmt.Errorf("failed to query queue: %w", err)
	}

	userIDs := make([]uint64, 0, len(result))
	inappropriateOutfitFlags := make(map[uint64]struct{}, len(result))
	inappropriateProfileFlags := make(map[uint64]struct{}, len(result))
	inappropriateFriendsFlags := make(map[uint64]struct{}, len(result))
	inappropriateGroupsFlags := make(map[uint64]struct{}, len(result))

	for _, row := range result {
		if userID, ok := row["user_id"].(float64); ok {
			userIDs = append(userIDs, uint64(userID))

			if row["inappropriate_outfit"].(float64) == 1 {
				inappropriateOutfitFlags[uint64(userID)] = struct{}{}
			}

			if row["inappropriate_profile"].(float64) == 1 {
				inappropriateProfileFlags[uint64(userID)] = struct{}{}
			}

			if row["inappropriate_friends"].(float64) == 1 {
				inappropriateFriendsFlags[uint64(userID)] = struct{}{}
			}

			if row["inappropriate_groups"].(float64) == 1 {
				inappropriateGroupsFlags[uint64(userID)] = struct{}{}
			}
		}
	}

	if len(userIDs) == 0 {
		return &UserBatch{
			UserIDs:                   []uint64{},
			InappropriateOutfitFlags:  make(map[uint64]struct{}),
			InappropriateProfileFlags: make(map[uint64]struct{}),
			InappropriateFriendsFlags: make(map[uint64]struct{}),
			InappropriateGroupsFlags:  make(map[uint64]struct{}),
		}, nil
	}

	// Then, mark them as processing
	updateQuery := "UPDATE queued_users SET processing = 1 WHERE user_id IN ("

	params := make([]any, len(userIDs))
	for i, id := range userIDs {
		if i > 0 {
			updateQuery += ","
		}

		updateQuery += "?"
		params[i] = id
	}

	updateQuery += ")"

	if _, err := c.api.ExecuteSQL(ctx, updateQuery, params); err != nil {
		return nil, fmt.Errorf("failed to mark users as processing: %w", err)
	}

	return &UserBatch{
		UserIDs:                   userIDs,
		InappropriateOutfitFlags:  inappropriateOutfitFlags,
		InappropriateProfileFlags: inappropriateProfileFlags,
		InappropriateFriendsFlags: inappropriateFriendsFlags,
		InappropriateGroupsFlags:  inappropriateGroupsFlags,
	}, nil
}

// MarkAsProcessed marks users as processed and not processing.
func (c *D1Client) MarkAsProcessed(ctx context.Context, userIDs []uint64, flaggedUsers map[uint64]struct{}) error {
	if len(userIDs) == 0 {
		return nil
	}

	// Build query with placeholders for all user IDs and their flagged status
	query := "UPDATE queued_users SET processed = 1, processing = 0, flagged = CASE user_id "
	params := make([]any, 0, len(userIDs))

	// Add CASE statement for each user ID
	for _, id := range userIDs {
		query += "WHEN ? THEN ? "

		params = append(params, id)
		if flaggedUsers != nil {
			if _, flagged := flaggedUsers[id]; flagged {
				params = append(params, 1)
				continue
			}
		}

		params = append(params, 0)
	}

	query += "END WHERE user_id IN ("

	// Add placeholders for WHERE clause
	for i, id := range userIDs {
		if i > 0 {
			query += ","
		}

		query += "?"

		params = append(params, id)
	}

	query += ")"

	_, err := c.api.ExecuteSQL(ctx, query, params)
	if err != nil {
		return fmt.Errorf("failed to mark users as processed: %w", err)
	}

	return nil
}

// CleanupQueue performs maintenance on the queue.
func (c *D1Client) CleanupQueue(ctx context.Context, stuckTimeout, retentionPeriod time.Duration) error {
	// Reset stuck processing items
	resetQuery := `
		UPDATE queued_users 
		SET processing = 0 
		WHERE processing = 1 
		AND processed = 0 
		AND queued_at < ?
	`

	stuckCutoff := time.Now().Add(-stuckTimeout).Unix()
	if _, err := c.api.ExecuteSQL(ctx, resetQuery, []any{stuckCutoff}); err != nil {
		return fmt.Errorf("failed to reset stuck processing: %w", err)
	}

	// Remove outdated processed records
	cleanupQuery := `
		DELETE FROM queued_users 
		WHERE processed = 1 
		AND queued_at < ?
	`

	retentionCutoff := time.Now().Add(-retentionPeriod).Unix()
	if _, err := c.api.ExecuteSQL(ctx, cleanupQuery, []any{retentionCutoff}); err != nil {
		return fmt.Errorf("failed to remove outdated records: %w", err)
	}

	return nil
}

// GetQueueStats retrieves current queue statistics.
func (c *D1Client) GetQueueStats(ctx context.Context) (*Stats, error) {
	// Get total items and processing count
	query := `
		SELECT 
			COUNT(*) as total,
			SUM(CASE WHEN processing = 1 THEN 1 ELSE 0 END) as processing,
			SUM(CASE WHEN processed = 0 AND processing = 0 THEN 1 ELSE 0 END) as unprocessed
		FROM queued_users
		WHERE processed = 0
	`

	result, err := c.api.ExecuteSQL(ctx, query, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get queue stats: %w", err)
	}

	if len(result) == 0 {
		return &Stats{}, nil
	}

	stats := &Stats{}
	if total, ok := result[0]["total"].(float64); ok {
		stats.TotalItems = int(total)
	}

	if processing, ok := result[0]["processing"].(float64); ok {
		stats.Processing = int(processing)
	}

	if unprocessed, ok := result[0]["unprocessed"].(float64); ok {
		stats.Unprocessed = int(unprocessed)
	}

	return stats, nil
}

// QueueUsers adds multiple users to the processing queue.
func (c *D1Client) QueueUsers(ctx context.Context, userIDs []uint64) (map[uint64]error, error) {
	if len(userIDs) == 0 {
		return nil, ErrEmptyBatch
	}

	if len(userIDs) > MaxQueueBatchSize {
		return nil, ErrBatchSizeExceeded
	}

	// Check existing users and their queue status
	checkQuery := `
		SELECT user_id, queued_at, processed 
		FROM queued_users 
		WHERE user_id IN (`

	params := make([]any, len(userIDs))
	for i, id := range userIDs {
		if i > 0 {
			checkQuery += ","
		}

		checkQuery += "?"
		params[i] = id
	}

	checkQuery += ")"

	result, err := c.api.ExecuteSQL(ctx, checkQuery, params)
	if err != nil {
		return nil, fmt.Errorf("failed to check queue status: %w", err)
	}

	// Track which users need to be inserted vs updated
	existingUsers := make(map[uint64]float64) // user_id -> queued_at
	for _, row := range result {
		if userID, ok := row["user_id"].(float64); ok {
			if queuedAt, ok := row["queued_at"].(float64); ok {
				existingUsers[uint64(userID)] = queuedAt
			}
		}
	}

	// Prepare batch insert for new users
	now := time.Now().Unix()
	insertQuery := `
		INSERT INTO queued_users (user_id, queued_at, processed, processing) 
		VALUES `

	updateQuery := `
		UPDATE queued_users 
		SET queued_at = ?, processed = 0, processing = 0 
		WHERE user_id IN (`

	var (
		insertParams []any
		updateParams []any
	)

	updateParams = append(updateParams, now) // First param is the new queued_at time

	cutoffTime := time.Now().AddDate(0, 0, -7).Unix()
	errors := make(map[uint64]error)

	for _, userID := range userIDs {
		if queuedAt, exists := existingUsers[userID]; exists {
			// Check if user was queued in the past 7 days
			if int64(queuedAt) > cutoffTime {
				errors[userID] = ErrUserRecentlyQueued
				continue
			}

			updateParams = append(updateParams, userID)
		} else {
			if len(insertParams) > 0 {
				insertQuery += ","
			}

			insertQuery += "(?, ?, 0, 0)"

			insertParams = append(insertParams, userID, now)
		}
	}

	// Execute insert for new users if any
	if len(insertParams) > 0 {
		if _, err := c.api.ExecuteSQL(ctx, insertQuery, insertParams); err != nil {
			return errors, fmt.Errorf("failed to insert queue entries: %w", err)
		}
	}

	// Execute update for existing users if any
	if len(updateParams) > 1 { // More than just the timestamp
		for i := range len(updateParams) - 1 {
			if i > 0 {
				updateQuery += ","
			}

			updateQuery += "?"
		}

		updateQuery += ")"

		if _, err := c.api.ExecuteSQL(ctx, updateQuery, updateParams); err != nil {
			return errors, fmt.Errorf("failed to update queue entries: %w", err)
		}
	}

	return errors, nil
}

// RemoveFromQueue removes an unprocessed user from the processing queue.
func (c *D1Client) RemoveFromQueue(ctx context.Context, userID uint64) error {
	// First check if the user is in an unprocessed state
	query := `
		SELECT processed, processing 
		FROM queued_users 
		WHERE user_id = ?
	`

	result, err := c.api.ExecuteSQL(ctx, query, []any{userID})
	if err != nil {
		return fmt.Errorf("failed to check queue status: %w", err)
	}

	if len(result) == 0 {
		return ErrUserNotFound
	}

	processed := result[0]["processed"].(float64)
	processing := result[0]["processing"].(float64)

	if processed == 1 || processing == 1 {
		return ErrUserProcessing
	}

	// Remove the unprocessed entry
	deleteQuery := "DELETE FROM queued_users WHERE user_id = ? AND processed = 0 AND processing = 0"
	if _, err := c.api.ExecuteSQL(ctx, deleteQuery, []any{userID}); err != nil {
		return fmt.Errorf("failed to remove queue entry: %w", err)
	}

	return nil
}

// GetQueueStatus retrieves the current status of a queued user.
func (c *D1Client) GetQueueStatus(ctx context.Context, userID uint64) (*Status, error) {
	query := `
		SELECT processing, processed, flagged, inappropriate_outfit 
		FROM queued_users 
		WHERE user_id = ?
	`

	result, err := c.api.ExecuteSQL(ctx, query, []any{userID})
	if err != nil {
		return nil, fmt.Errorf("failed to get queue status: %w", err)
	}

	if len(result) == 0 {
		return nil, ErrUserNotFound
	}

	var status Status

	status.Processing = result[0]["processing"].(float64) == 1
	status.Processed = result[0]["processed"].(float64) == 1
	status.Flagged = result[0]["flagged"].(float64) == 1
	status.InappropriateOutfit = result[0]["inappropriate_outfit"].(float64) == 1

	return &status, nil
}

// UpdateIPTrackingUserFlagged updates the queue_ip_tracking table for the specified users based on flagged status.
func (c *D1Client) UpdateIPTrackingUserFlagged(ctx context.Context, userFlaggedStatus map[uint64]bool) error {
	if len(userFlaggedStatus) == 0 {
		return nil
	}

	// Build query with CASE statement to update user_flagged based on user_id
	query := "UPDATE queue_ip_tracking SET user_flagged = CASE user_id "
	params := make([]any, 0, len(userFlaggedStatus)*3) // *3 to account for CASE + WHERE params

	// Add CASE statement for each user ID
	for userID, flagged := range userFlaggedStatus {
		query += "WHEN ? THEN ? "

		params = append(params, userID)
		if flagged {
			params = append(params, 1)
		} else {
			params = append(params, 0)
		}
	}

	query += "END WHERE user_id IN ("

	// Add placeholders for WHERE clause
	userIDs := make([]uint64, 0, len(userFlaggedStatus))
	for userID := range userFlaggedStatus {
		userIDs = append(userIDs, userID)
	}

	for i, userID := range userIDs {
		if i > 0 {
			query += ","
		}

		query += "?"

		params = append(params, userID)
	}

	query += ") AND user_flagged IS NULL" // Only update if not already set

	result, err := c.api.ExecuteSQL(ctx, query, params)
	if err != nil {
		return fmt.Errorf("failed to update IP tracking user flagged status: %w", err)
	}

	c.logger.Debug("Updated IP tracking user flagged status",
		zap.Int("users_processed", len(userFlaggedStatus)),
		zap.Int("rows_affected", len(result)))

	return nil
}

// CleanupIPTracking removes old IP tracking records to prevent table bloat.
func (c *D1Client) CleanupIPTracking(ctx context.Context, retentionPeriod time.Duration) error {
	// Remove IP tracking records older than retention period
	cleanupQuery := `
		DELETE FROM queue_ip_tracking 
		WHERE queued_at < ?
	`

	retentionCutoff := time.Now().Add(-retentionPeriod).Unix()

	result, err := c.api.ExecuteSQL(ctx, cleanupQuery, []any{retentionCutoff})
	if err != nil {
		return fmt.Errorf("failed to cleanup IP tracking records: %w", err)
	}

	c.logger.Debug("Cleaned up old IP tracking records",
		zap.Int64("retention_cutoff", retentionCutoff),
		zap.Int("records_removed", len(result)))

	return nil
}

// AddFlaggedUsers inserts flagged users into the user_flags table.
func (c *D1Client) AddFlaggedUsers(ctx context.Context, flaggedUsers map[uint64]*types.ReviewUser) error {
	if len(flaggedUsers) == 0 {
		return nil
	}

	// Convert map to slice
	userIDs := make([]uint64, 0, len(flaggedUsers))
	for userID := range flaggedUsers {
		userIDs = append(userIDs, userID)
	}

	totalProcessed := 0

	// Process users in batches
	for i := 0; i < len(userIDs); i += MaxFlaggedUsersBatchSize {
		end := min(i+MaxFlaggedUsersBatchSize, len(userIDs))
		batchUserIDs := userIDs[i:end]

		// Build batch insert query for this batch
		sqlStmt := `
			INSERT OR REPLACE INTO user_flags (
				user_id, 
				flag_type, 
				confidence, 
				reasons,
				engine_version
			) VALUES
		`

		params := make([]any, 0, len(batchUserIDs)*5)

		for j, userID := range batchUserIDs {
			user := flaggedUsers[userID]

			if j > 0 {
				sqlStmt += ","
			}

			sqlStmt += "(?, ?, ?, ?, ?)"

			// Convert reasons to JSON string
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

				if jsonBytes, err := json.Marshal(reasonsData); err == nil {
					reasonsJSON = string(jsonBytes)
				}
			}

			params = append(params,
				userID,
				enum.UserTypeFlagged,
				user.Confidence,
				reasonsJSON,
				user.EngineVersion,
			)
		}

		// Execute the batch
		_, err := c.api.ExecuteSQL(ctx, sqlStmt, params)
		if err != nil {
			return fmt.Errorf("failed to insert flagged users batch %d-%d: %w", i, end-1, err)
		}

		totalProcessed += len(batchUserIDs)

		c.logger.Debug("Inserted flagged users batch",
			zap.Int("batch_start", i),
			zap.Int("batch_end", end-1),
			zap.Int("batch_count", len(batchUserIDs)))
	}

	c.logger.Debug("Finished inserting flagged users into user_flags table",
		zap.Int("total_count", totalProcessed))

	return nil
}

// AddConfirmedUser inserts or updates a confirmed user in the user_flags table.
func (c *D1Client) AddConfirmedUser(ctx context.Context, user *types.ReviewUser, reviewerID uint64) error {
	if user == nil {
		return nil
	}

	// Fetch reviewer information from the database
	reviewerUsername := "Unknown"
	reviewerDisplayName := "Unknown"

	if reviewerInfos, err := c.db.Model().Reviewer().GetReviewerInfos(ctx, []uint64{reviewerID}); err != nil {
		c.logger.Warn("Failed to get reviewer info, using fallback values",
			zap.Error(err),
			zap.Uint64("reviewer_id", reviewerID))
	} else if reviewerInfo, exists := reviewerInfos[reviewerID]; exists {
		reviewerUsername = reviewerInfo.Username
		reviewerDisplayName = reviewerInfo.DisplayName
	}

	// Convert reasons to JSON string
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

		if jsonBytes, err := json.Marshal(reasonsData); err == nil {
			reasonsJSON = string(jsonBytes)
		}
	}

	// Insert or replace the user in the user_flags table
	sqlStmt := `
		INSERT OR REPLACE INTO user_flags (
			user_id, 
			flag_type, 
			confidence, 
			reasons,
			reviewer_id,
			reviewer_username,
			reviewer_display_name,
			engine_version
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := c.api.ExecuteSQL(ctx, sqlStmt, []any{
		user.ID,
		enum.UserTypeConfirmed,
		user.Confidence,
		reasonsJSON,
		reviewerID,
		reviewerUsername,
		reviewerDisplayName,
		user.EngineVersion,
	})
	if err != nil {
		return fmt.Errorf("failed to insert confirmed user: %w", err)
	}

	c.logger.Debug("Added confirmed user to user_flags table",
		zap.Uint64("user_id", user.ID),
		zap.Float64("confidence", user.Confidence),
		zap.Uint64("reviewer_id", reviewerID),
		zap.String("reviewer_username", reviewerUsername))

	return nil
}

// RemoveUser removes a user from the user_flags table.
func (c *D1Client) RemoveUser(ctx context.Context, userID uint64) error {
	sqlStmt := "DELETE FROM user_flags WHERE user_id = ?"

	_, err := c.api.ExecuteSQL(ctx, sqlStmt, []any{userID})
	if err != nil {
		return fmt.Errorf("failed to remove user from user_flags: %w", err)
	}

	c.logger.Debug("Removed user from user_flags table",
		zap.Uint64("user_id", userID))

	return nil
}

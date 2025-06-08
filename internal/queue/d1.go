package queue

import (
	"context"
	"fmt"
	"time"

	"github.com/robalyx/rotector/internal/setup"
	"go.uber.org/zap"
)

const (
	// MaxQueueBatchSize is the maximum number of users that can be queued in a single batch.
	MaxQueueBatchSize = 100
)

// Stats represents queue statistics.
type Stats struct {
	TotalItems  int
	Processing  int
	Unprocessed int
}

// Status represents the current status of a queued user.
type Status struct {
	Processing bool
	Processed  bool
	Flagged    bool
}

// D1Client handles Cloudflare D1 API requests.
type D1Client struct {
	api    *CloudflareAPI
	logger *zap.Logger
}

// NewD1Client creates a new D1 client.
func NewD1Client(app *setup.App, logger *zap.Logger) *D1Client {
	api := NewCloudflareAPI(
		app.Config.Worker.Cloudflare.AccountID,
		app.Config.Worker.Cloudflare.DatabaseID,
		app.Config.Worker.Cloudflare.APIToken,
		app.Config.Worker.Cloudflare.APIEndpoint,
	)

	return &D1Client{
		api:    api,
		logger: logger.Named("d1_client"),
	}
}

// GetNextBatch retrieves the next batch of unprocessed and non-processing users.
func (c *D1Client) GetNextBatch(ctx context.Context, batchSize int) ([]uint64, error) {
	// First, get the batch of users
	selectQuery := `
		SELECT user_id 
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
	for _, row := range result {
		if userID, ok := row["user_id"].(float64); ok {
			userIDs = append(userIDs, uint64(userID))
		}
	}

	if len(userIDs) == 0 {
		return nil, nil
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

	return userIDs, nil
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

	var insertParams []any
	var updateParams []any
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

	processed, _ := result[0]["processed"].(float64)
	processing, _ := result[0]["processing"].(float64)

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
		SELECT processing, processed, flagged 
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
	if processing, ok := result[0]["processing"].(float64); ok {
		status.Processing = processing == 1
	}
	if processed, ok := result[0]["processed"].(float64); ok {
		status.Processed = processed == 1
	}
	if flagged, ok := result[0]["flagged"].(float64); ok {
		status.Flagged = flagged == 1
	}

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

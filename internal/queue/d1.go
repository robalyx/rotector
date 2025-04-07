package queue

import (
	"context"
	"fmt"
	"time"

	"github.com/robalyx/rotector/internal/setup"
	"go.uber.org/zap"
)

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

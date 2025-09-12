package manager

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/robalyx/rotector/internal/cloudflare/api"
	"go.uber.org/zap"
)

const (
	// MaxQueueBatchSize is the maximum number of users that can be queued in a single batch.
	MaxQueueBatchSize = 50
)

var (
	// ErrUserNotFound indicates the user was not found in the cloudflare.
	ErrUserNotFound = errors.New("user not found in cloudflare")
	// ErrUserRecentlyQueued indicates the user was queued within the past 7 days.
	ErrUserRecentlyQueued = errors.New("user was recently queued")
	// ErrUserProcessing indicates the user is currently being processed.
	ErrUserProcessing = errors.New("cannot remove user that is already processed or being processed")
	// ErrBatchSizeExceeded indicates the batch size exceeds the maximum allowed.
	ErrBatchSizeExceeded = errors.New("batch size exceeds maximum capacity")
	// ErrEmptyBatch indicates an empty batch was provided.
	ErrEmptyBatch = errors.New("empty batch provided")
)

// Status represents the current status of a queued user.
type Status struct {
	Processing          bool
	Processed           bool
	Flagged             bool
	InappropriateOutfit bool
}

// Stats represents cloudflare statistics.
type Stats struct {
	TotalItems  int
	Processing  int
	Unprocessed int
}

// UserBatch represents a batch of users with their inappropriate flags.
type UserBatch struct {
	UserIDs                   []int64
	InappropriateOutfitFlags  map[int64]struct{}
	InappropriateProfileFlags map[int64]struct{}
	InappropriateFriendsFlags map[int64]struct{}
	InappropriateGroupsFlags  map[int64]struct{}
}

// Queue handles core cloudflare operations.
type Queue struct {
	d1     *api.D1Client
	logger *zap.Logger
}

// NewQueue creates a new cloudflare manager.
func NewQueue(d1Client *api.D1Client, logger *zap.Logger) *Queue {
	return &Queue{
		d1:     d1Client,
		logger: logger,
	}
}

// GetNextBatch retrieves the next batch of unprocessed and non-processing users.
func (q *Queue) GetNextBatch(ctx context.Context, batchSize int) (*UserBatch, error) {
	// First, get the batch of users
	selectQuery := `
		SELECT user_id, inappropriate_outfit, inappropriate_profile, inappropriate_friends, inappropriate_groups 
		FROM queued_users 
		WHERE processed = 0 AND processing = 0
		ORDER BY queued_at ASC 
		LIMIT ?
	`

	result, err := q.d1.ExecuteSQL(ctx, selectQuery, []any{batchSize})
	if err != nil {
		return nil, fmt.Errorf("failed to query cloudflare: %w", err)
	}

	userIDs := make([]int64, 0, len(result))
	inappropriateOutfitFlags := make(map[int64]struct{}, len(result))
	inappropriateProfileFlags := make(map[int64]struct{}, len(result))
	inappropriateFriendsFlags := make(map[int64]struct{}, len(result))
	inappropriateGroupsFlags := make(map[int64]struct{}, len(result))

	for _, row := range result {
		if userID, ok := row["user_id"].(float64); ok {
			userIDs = append(userIDs, int64(userID))

			if row["inappropriate_outfit"].(float64) == 1 {
				inappropriateOutfitFlags[int64(userID)] = struct{}{}
			}

			if row["inappropriate_profile"].(float64) == 1 {
				inappropriateProfileFlags[int64(userID)] = struct{}{}
			}

			if row["inappropriate_friends"].(float64) == 1 {
				inappropriateFriendsFlags[int64(userID)] = struct{}{}
			}

			if row["inappropriate_groups"].(float64) == 1 {
				inappropriateGroupsFlags[int64(userID)] = struct{}{}
			}
		}
	}

	if len(userIDs) == 0 {
		return &UserBatch{
			UserIDs:                   []int64{},
			InappropriateOutfitFlags:  make(map[int64]struct{}),
			InappropriateProfileFlags: make(map[int64]struct{}),
			InappropriateFriendsFlags: make(map[int64]struct{}),
			InappropriateGroupsFlags:  make(map[int64]struct{}),
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

	if _, err := q.d1.ExecuteSQL(ctx, updateQuery, params); err != nil {
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
func (q *Queue) MarkAsProcessed(ctx context.Context, userIDs []int64, flaggedUsers map[int64]struct{}) error {
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

	_, err := q.d1.ExecuteSQL(ctx, query, params)
	if err != nil {
		return fmt.Errorf("failed to mark users as processed: %w", err)
	}

	return nil
}

// Cleanup performs maintenance on the cloudflare.
func (q *Queue) Cleanup(ctx context.Context, stuckTimeout, retentionPeriod time.Duration) error {
	// Reset stuck processing items
	resetQuery := `
		UPDATE queued_users 
		SET processing = 0 
		WHERE processing = 1 
		AND processed = 0 
		AND queued_at < ?
	`

	stuckCutoff := time.Now().Add(-stuckTimeout).Unix()
	if _, err := q.d1.ExecuteSQL(ctx, resetQuery, []any{stuckCutoff}); err != nil {
		return fmt.Errorf("failed to reset stuck processing: %w", err)
	}

	// Remove outdated processed records
	cleanupQuery := `
		DELETE FROM queued_users 
		WHERE processed = 1 
		AND queued_at < ?
	`

	retentionCutoff := time.Now().Add(-retentionPeriod).Unix()
	if _, err := q.d1.ExecuteSQL(ctx, cleanupQuery, []any{retentionCutoff}); err != nil {
		return fmt.Errorf("failed to remove outdated records: %w", err)
	}

	return nil
}

// GetStats retrieves current cloudflare statistics.
func (q *Queue) GetStats(ctx context.Context) (*Stats, error) {
	// Get total items and processing count
	query := `
		SELECT 
			COUNT(*) as total,
			SUM(CASE WHEN processing = 1 THEN 1 ELSE 0 END) as processing,
			SUM(CASE WHEN processed = 0 AND processing = 0 THEN 1 ELSE 0 END) as unprocessed
		FROM queued_users
		WHERE processed = 0
	`

	result, err := q.d1.ExecuteSQL(ctx, query, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get cloudflare stats: %w", err)
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

// AddUsers adds multiple users to the processing cloudflare.
func (q *Queue) AddUsers(ctx context.Context, userIDs []int64) (map[int64]error, error) {
	if len(userIDs) == 0 {
		return nil, ErrEmptyBatch
	}

	if len(userIDs) > MaxQueueBatchSize {
		return nil, ErrBatchSizeExceeded
	}

	// Check existing users and their cloudflare status
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

	result, err := q.d1.ExecuteSQL(ctx, checkQuery, params)
	if err != nil {
		return nil, fmt.Errorf("failed to check cloudflare status: %w", err)
	}

	// Track which users need to be inserted vs updated
	existingUsers := make(map[int64]float64) // user_id -> queued_at
	for _, row := range result {
		if userID, ok := row["user_id"].(float64); ok {
			if queuedAt, ok := row["queued_at"].(float64); ok {
				existingUsers[int64(userID)] = queuedAt
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
	errors := make(map[int64]error)

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
		if _, err := q.d1.ExecuteSQL(ctx, insertQuery, insertParams); err != nil {
			return errors, fmt.Errorf("failed to insert cloudflare entries: %w", err)
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

		if _, err := q.d1.ExecuteSQL(ctx, updateQuery, updateParams); err != nil {
			return errors, fmt.Errorf("failed to update cloudflare entries: %w", err)
		}
	}

	return errors, nil
}

// Remove removes an unprocessed user from the processing cloudflare.
func (q *Queue) Remove(ctx context.Context, userID int64) error {
	// First check if the user is in an unprocessed state
	query := `
		SELECT processed, processing 
		FROM queued_users 
		WHERE user_id = ?
	`

	result, err := q.d1.ExecuteSQL(ctx, query, []any{userID})
	if err != nil {
		return fmt.Errorf("failed to check cloudflare status: %w", err)
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
	if _, err := q.d1.ExecuteSQL(ctx, deleteQuery, []any{userID}); err != nil {
		return fmt.Errorf("failed to remove cloudflare entry: %w", err)
	}

	return nil
}

// GetStatus retrieves the current status of a queued user.
func (q *Queue) GetStatus(ctx context.Context, userID int64) (*Status, error) {
	query := `
		SELECT processing, processed, flagged, inappropriate_outfit 
		FROM queued_users 
		WHERE user_id = ?
	`

	result, err := q.d1.ExecuteSQL(ctx, query, []any{userID})
	if err != nil {
		return nil, fmt.Errorf("failed to get cloudflare status: %w", err)
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

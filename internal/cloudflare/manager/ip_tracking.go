package manager

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/robalyx/rotector/internal/cloudflare/api"
	"go.uber.org/zap"
)

// IPTracking handles IP tracking operations.
type IPTracking struct {
	d1     *api.D1Client
	logger *zap.Logger
}

// NewIPTracking creates a new IP tracking manager.
func NewIPTracking(d1Client *api.D1Client, logger *zap.Logger) *IPTracking {
	return &IPTracking{
		d1:     d1Client,
		logger: logger,
	}
}

// UpdateUserFlagged updates the queue_ip_tracking table for the specified users based on flagged status.
func (i *IPTracking) UpdateUserFlagged(ctx context.Context, userFlaggedStatus map[int64]bool) error {
	if len(userFlaggedStatus) == 0 {
		return nil
	}

	// Build query with CASE statement to update user_flagged based on user_id
	query := "UPDATE queue_ip_tracking SET user_flagged = CASE user_id "
	params := make([]any, 0, len(userFlaggedStatus)*3) // *3 to account for CASE + WHERE params

	// Add CASE statement for each user ID
	var caseWhenBuilder strings.Builder
	for userID, flagged := range userFlaggedStatus {
		caseWhenBuilder.WriteString("WHEN ? THEN ? ")

		params = append(params, userID)
		if flagged {
			params = append(params, 1)
		} else {
			params = append(params, 0)
		}
	}

	query += caseWhenBuilder.String()

	query += "END WHERE user_id IN ("

	// Add placeholders for WHERE clause
	userIDs := make([]int64, 0, len(userFlaggedStatus))
	for userID := range userFlaggedStatus {
		userIDs = append(userIDs, userID)
	}

	var whereInPlaceholders strings.Builder

	for i, userID := range userIDs {
		if i > 0 {
			whereInPlaceholders.WriteString(",")
		}

		whereInPlaceholders.WriteString("?")

		params = append(params, userID)
	}

	query += whereInPlaceholders.String()

	query += ") AND user_flagged IS NULL" // Only update if not already set

	result, err := i.d1.ExecuteSQL(ctx, query, params)
	if err != nil {
		return fmt.Errorf("failed to update IP tracking user flagged status: %w", err)
	}

	i.logger.Debug("Updated IP tracking user flagged status",
		zap.Int("users_processed", len(userFlaggedStatus)),
		zap.Int("rows_affected", len(result)))

	return nil
}

// Cleanup removes old IP tracking records to prevent table bloat.
func (i *IPTracking) Cleanup(ctx context.Context, retentionPeriod time.Duration) error {
	// Remove IP tracking records older than retention period
	cleanupQuery := `
		DELETE FROM queue_ip_tracking
		WHERE queued_at < ?
	`

	retentionCutoff := time.Now().Add(-retentionPeriod).Unix()

	result, err := i.d1.ExecuteSQL(ctx, cleanupQuery, []any{retentionCutoff})
	if err != nil {
		return fmt.Errorf("failed to cleanup IP tracking records: %w", err)
	}

	i.logger.Debug("Cleaned up old IP tracking records",
		zap.Int64("retention_cutoff", retentionCutoff),
		zap.Int("records_removed", len(result)))

	return nil
}

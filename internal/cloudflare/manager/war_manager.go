package manager

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/robalyx/rotector/internal/cloudflare/api"
	"go.uber.org/zap"
)

// CandidateTarget represents a potential target user.
type CandidateTarget struct {
	UserID     int64   `json:"userId"`
	UserName   string  `json:"userName"`
	UserStatus int     `json:"userStatus"`
	Confidence float64 `json:"confidence"`
	ZoneID     int64   `json:"zoneId"`
}

// ExtensionReport represents an extension user's report of a Roblox user.
type ExtensionReport struct {
	ID                int64  `json:"id"`
	ExtensionUserUUID string `json:"extensionUserUuid"`
	ReportedUserID    int64  `json:"reportedUserId"`
	ReportReason      string `json:"reportReason"`
	Status            string `json:"status"`
	PointsAwarded     int    `json:"pointsAwarded"`
	ReportedAt        string `json:"reportedAt"`
	ProcessedAt       string `json:"processedAt"`
}

// MajorOrderInfo represents basic major order information.
type MajorOrderInfo struct {
	ID           int64 `json:"id"`
	TargetValue  int64 `json:"targetValue"`
	CurrentValue int64 `json:"currentValue"`
	StartValue   int64 `json:"startValue"`
}

// WarManager handles war map operations and target management.
type WarManager struct {
	d1     *api.D1Client
	logger *zap.Logger
}

// NewWarManager creates a new war manager.
func NewWarManager(d1Client *api.D1Client, logger *zap.Logger) *WarManager {
	return &WarManager{
		d1:     d1Client,
		logger: logger,
	}
}

// AddActiveTarget adds a user as a global active target.
func (w *WarManager) AddActiveTarget(
	ctx context.Context, userID int64, zoneID int64, userName string, userStatus int, confidence float64,
) error {
	// Target expires in 24 hours
	expiresAt := time.Now().Add(24 * time.Hour)

	sql := `
		INSERT INTO active_targets
		(user_id, zone_id, user_name, user_status, confidence, assigned_at, expires_at, is_active)
		VALUES (?, ?, ?, ?, ?, datetime('now'), ?, 1)
		ON CONFLICT(user_id) DO UPDATE SET
			zone_id = excluded.zone_id,
			user_name = excluded.user_name,
			user_status = excluded.user_status,
			confidence = excluded.confidence,
			assigned_at = excluded.assigned_at,
			expires_at = excluded.expires_at,
			is_active = 1
	`

	_, err := w.d1.ExecuteSQL(ctx, sql, []any{
		userID, zoneID, userName, userStatus, confidence, expiresAt.Format("2006-01-02 15:04:05"),
	})
	if err != nil {
		return fmt.Errorf("failed to add active target: %w", err)
	}

	w.logger.Info("Added global active target",
		zap.Int64("userID", userID),
		zap.Int64("zoneID", zoneID),
		zap.String("userName", userName),
		zap.Int("userStatus", userStatus),
		zap.Float64("confidence", confidence))

	return nil
}

// RemoveActiveTarget removes a user from active targets.
func (w *WarManager) RemoveActiveTarget(ctx context.Context, userID int64, reason string) error {
	sql := `UPDATE active_targets SET is_active = 0 WHERE user_id = ?`

	_, err := w.d1.ExecuteSQL(ctx, sql, []any{userID})
	if err != nil {
		return fmt.Errorf("failed to remove active target: %w", err)
	}

	w.logger.Info("Removed active target",
		zap.Int64("userID", userID),
		zap.String("reason", reason))

	return nil
}

// GetExpiredTargets returns targets that have expired (older than 24 hours).
func (w *WarManager) GetExpiredTargets(ctx context.Context) ([]int64, error) {
	sql := `
		SELECT user_id FROM active_targets
		WHERE is_active = 1 AND expires_at < datetime('now')
	`

	results, err := w.d1.ExecuteSQL(ctx, sql, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get expired targets: %w", err)
	}

	userIDs := make([]int64, 0, len(results))
	for _, row := range results {
		userIDs = append(userIDs, int64(row["user_id"].(float64)))
	}

	return userIDs, nil
}

// GetGlobalTargetCount returns the total number of active targets globally.
func (w *WarManager) GetGlobalTargetCount(ctx context.Context) (int, error) {
	sql := `SELECT COUNT(*) as count FROM active_targets WHERE is_active = 1`

	results, err := w.d1.ExecuteSQL(ctx, sql, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to get global target count: %w", err)
	}

	if len(results) == 0 {
		return 0, nil
	}

	return int(results[0]["count"].(float64)), nil
}

// UpdateZoneStats updates the user statistics for a zone.
func (w *WarManager) UpdateZoneStats(ctx context.Context, zoneID int64, totalUsers, bannedUsers, flaggedUsers, confirmedUsers int64) error {
	// Calculate liberation percentage (banned users out of total users)
	var liberation float64
	if totalUsers > 0 {
		liberation = (float64(bannedUsers) / float64(totalUsers)) * 100
		if liberation > 100 {
			liberation = 100
		}
	}

	sql := `
		UPDATE war_zones SET
			total_users = ?,
			banned_users = ?,
			flagged_users = ?,
			confirmed_users = ?,
			liberation = ?,
			updated_at = datetime('now')
		WHERE id = ?
	`

	_, err := w.d1.ExecuteSQL(ctx, sql, []any{
		totalUsers, bannedUsers, flaggedUsers, confirmedUsers, liberation, zoneID,
	})
	if err != nil {
		return fmt.Errorf("failed to update zone stats: %w", err)
	}

	w.logger.Debug("Updated zone stats",
		zap.Int64("zoneID", zoneID),
		zap.Int64("totalUsers", totalUsers),
		zap.Int64("bannedUsers", bannedUsers),
		zap.Float64("liberation", liberation))

	return nil
}

// UpdateMajorOrderProgress updates the progress of active major orders.
func (w *WarManager) UpdateMajorOrderProgress(ctx context.Context) error {
	// Get active major orders of type 'ban_count'
	sql := `SELECT id, current_value, target_value FROM major_orders WHERE is_active = 1 AND type = 'ban_count'`

	results, err := w.d1.ExecuteSQL(ctx, sql, nil)
	if err != nil {
		return fmt.Errorf("failed to get active major orders: %w", err)
	}

	for _, row := range results {
		orderID := int64(row["id"].(float64))

		// Get total banned users across all zones
		banCountSQL := `SELECT SUM(banned_users) as total_banned FROM war_zones WHERE is_active = 1`

		banResults, err := w.d1.ExecuteSQL(ctx, banCountSQL, nil)
		if err != nil {
			w.logger.Error("Failed to get ban count for major order", zap.Int64("orderID", orderID), zap.Error(err))
			continue
		}

		var totalBanned int64
		if len(banResults) > 0 && banResults[0]["total_banned"] != nil {
			totalBanned = int64(banResults[0]["total_banned"].(float64))
		}

		// Check if order is completed
		targetValue := int64(row["target_value"].(float64))
		isCompleted := totalBanned >= targetValue

		// Update major order progress and completion status
		updateSQL := `
			UPDATE major_orders SET
				current_value = ?,
				is_active = CASE
					WHEN ? >= target_value THEN 0
					ELSE is_active
				END,
				completed_at = CASE
					WHEN ? >= target_value AND completed_at IS NULL THEN datetime('now')
					ELSE completed_at
				END,
				updated_at = datetime('now')
			WHERE id = ?
		`

		_, err = w.d1.ExecuteSQL(ctx, updateSQL, []any{totalBanned, totalBanned, totalBanned, orderID})
		if err != nil {
			w.logger.Error("Failed to update major order progress", zap.Int64("orderID", orderID), zap.Error(err))
		} else if isCompleted {
			w.logger.Info("Major order completed",
				zap.Int64("orderID", orderID),
				zap.Int64("targetValue", targetValue),
				zap.Int64("currentValue", totalBanned))
		}
	}

	return nil
}

// GetPendingExtensionReports gets all extension reports with status 'pending'.
func (w *WarManager) GetPendingExtensionReports(ctx context.Context) ([]ExtensionReport, error) {
	sql := `
		SELECT er.id, eu.uuid as extension_user_uuid, er.reported_user_id, er.report_reason,
		       er.status, er.points_awarded, er.reported_at, er.processed_at
		FROM extension_reports er
		INNER JOIN extension_users eu ON er.extension_user_id = eu.id
		WHERE er.status = 'pending'
		ORDER BY er.reported_at ASC
		LIMIT 100
	`

	results, err := w.d1.ExecuteSQL(ctx, sql, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get pending extension reports: %w", err)
	}

	reports := make([]ExtensionReport, 0, len(results))
	for _, row := range results {
		var reportReason, processedAt string
		if row["report_reason"] != nil {
			reportReason = row["report_reason"].(string)
		}

		if row["processed_at"] != nil {
			processedAt = row["processed_at"].(string)
		}

		report := ExtensionReport{
			ID:                int64(row["id"].(float64)),
			ExtensionUserUUID: row["extension_user_uuid"].(string),
			ReportedUserID:    int64(row["reported_user_id"].(float64)),
			ReportReason:      reportReason,
			Status:            row["status"].(string),
			PointsAwarded:     int(row["points_awarded"].(float64)),
			ReportedAt:        row["reported_at"].(string),
			ProcessedAt:       processedAt,
		}
		reports = append(reports, report)
	}

	return reports, nil
}

// CheckUserInFlags checks if a user exists in the user_flags table.
func (w *WarManager) CheckUserInFlags(ctx context.Context, userID int64) (bool, error) {
	sql := `SELECT flag_type FROM user_flags WHERE user_id = ?`

	results, err := w.d1.ExecuteSQL(ctx, sql, []any{userID})
	if err != nil {
		return false, fmt.Errorf("failed to check user in flags: %w", err)
	}

	if len(results) == 0 {
		return false, nil
	}

	return true, nil
}

// IsFirstReporter checks if this extension user was the first to report this Roblox user.
func (w *WarManager) IsFirstReporter(ctx context.Context, reportedUserID int64, extensionUserUUID string) (bool, error) {
	sql := `
		SELECT eu.uuid as extension_user_uuid
		FROM extension_reports er
		INNER JOIN extension_users eu ON er.extension_user_id = eu.id
		WHERE er.reported_user_id = ?
		ORDER BY er.reported_at ASC
		LIMIT 1
	`

	results, err := w.d1.ExecuteSQL(ctx, sql, []any{reportedUserID})
	if err != nil {
		return false, fmt.Errorf("failed to check first reporter: %w", err)
	}

	if len(results) == 0 {
		return true, nil // This is the first report
	}

	firstReporterUUID := results[0]["extension_user_uuid"].(string)

	return firstReporterUUID == extensionUserUUID, nil
}

// UpdateExtensionReport updates an extension report's status and points awarded.
func (w *WarManager) UpdateExtensionReport(ctx context.Context, reportID int64, status string, pointsAwarded int) error {
	sql := `
		UPDATE extension_reports
		SET status = ?, points_awarded = ?, processed_at = datetime('now')
		WHERE id = ?
	`

	_, err := w.d1.ExecuteSQL(ctx, sql, []any{status, pointsAwarded, reportID})
	if err != nil {
		return fmt.Errorf("failed to update extension report: %w", err)
	}

	return nil
}

// AwardPointsToExtensionUser adds points to an extension user's total.
func (w *WarManager) AwardPointsToExtensionUser(ctx context.Context, extensionUserUUID string, points int) error {
	sql := `
		UPDATE extension_users
		SET total_points = total_points + ?, last_active = datetime('now')
		WHERE uuid = ?
	`

	_, err := w.d1.ExecuteSQL(ctx, sql, []any{points, extensionUserUUID})
	if err != nil {
		return fmt.Errorf("failed to award points to extension user: %w", err)
	}

	return nil
}

// GetActiveMajorOrders gets all active major orders.
func (w *WarManager) GetActiveMajorOrders(ctx context.Context) ([]MajorOrderInfo, error) {
	sql := `
		SELECT id, target_value, current_value, start_value
		FROM major_orders
		WHERE is_active = 1 AND expires_at > datetime('now')
	`

	results, err := w.d1.ExecuteSQL(ctx, sql, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get active major orders: %w", err)
	}

	orders := make([]MajorOrderInfo, 0, len(results))
	for _, row := range results {
		order := MajorOrderInfo{
			ID:           int64(row["id"].(float64)),
			TargetValue:  int64(row["target_value"].(float64)),
			CurrentValue: int64(row["current_value"].(float64)),
			StartValue:   int64(row["start_value"].(float64)),
		}
		orders = append(orders, order)
	}

	return orders, nil
}

// GetActiveTargetUserIDs gets all user IDs from active targets.
func (w *WarManager) GetActiveTargetUserIDs(ctx context.Context) ([]int64, error) {
	sql := `SELECT user_id FROM active_targets WHERE is_active = 1`

	results, err := w.d1.ExecuteSQL(ctx, sql, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get active target user IDs: %w", err)
	}

	userIDs := make([]int64, 0, len(results))
	for _, row := range results {
		userIDs = append(userIDs, int64(row["user_id"].(float64)))
	}

	return userIDs, nil
}

// GetRecentTargetUserIDs gets user IDs that were active targets within the last N days (cooldown period).
func (w *WarManager) GetRecentTargetUserIDs(ctx context.Context, days int) ([]int64, error) {
	sql := `
		SELECT DISTINCT user_id
		FROM active_targets
		WHERE assigned_at > datetime('now', '-' || ? || ' days')
	`

	results, err := w.d1.ExecuteSQL(ctx, sql, []any{days})
	if err != nil {
		return nil, fmt.Errorf("failed to get recent target user IDs: %w", err)
	}

	userIDs := make([]int64, 0, len(results))
	for _, row := range results {
		userIDs = append(userIDs, int64(row["user_id"].(float64)))
	}

	return userIDs, nil
}

// RemoveUserFromWarSystem removes a user from the war system.
func (w *WarManager) RemoveUserFromWarSystem(ctx context.Context, userID int64) error {
	return w.RemoveUsersFromWarSystem(ctx, []int64{userID})
}

// RemoveUsersFromWarSystem removes multiple users from the war system in batches.
func (w *WarManager) RemoveUsersFromWarSystem(ctx context.Context, userIDs []int64) error {
	if len(userIDs) == 0 {
		return nil
	}

	const maxBatchSize = 50

	// Process deletions in batches
	for i := 0; i < len(userIDs); i += maxBatchSize {
		end := min(i+maxBatchSize, len(userIDs))

		batchUserIDs := userIDs[i:end]

		// Build IN clause parameters
		inClause := ""

		params := make([]any, len(batchUserIDs))

		var inClausePlaceholders strings.Builder

		for j, userID := range batchUserIDs {
			if j > 0 {
				inClausePlaceholders.WriteString(",")
			}

			inClausePlaceholders.WriteString("?")

			params[j] = userID
		}

		inClause += inClausePlaceholders.String()

		// Remove active target assignments
		activeTargetsQuery := "DELETE FROM active_targets WHERE user_id IN (" + inClause + ")"
		if _, err := w.d1.ExecuteSQL(ctx, activeTargetsQuery, params); err != nil {
			return fmt.Errorf("failed to cleanup active_targets for user batch %d-%d: %w", i, end-1, err)
		}

		// NOTE: Extension reports are preserved for audit purposes
	}

	w.logger.Info("Removed users from war system",
		zap.Int("userCount", len(userIDs)))

	return nil
}

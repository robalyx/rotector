package manager

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/robalyx/rotector/internal/cloudflare/api"
	"go.uber.org/zap"
)

// LeaderboardEntry represents a single user's leaderboard position.
type LeaderboardEntry struct {
	Rank         int     `json:"rank"`
	DisplayName  string  `json:"displayName"`
	TotalPoints  int     `json:"totalPoints"`
	TotalReports int     `json:"totalReports"`
	SuccessRate  float64 `json:"successRate"`
	IsAnonymous  bool    `json:"isAnonymous"`
}

// LeaderboardData represents the complete leaderboard response.
type LeaderboardData struct {
	Leaderboard []LeaderboardEntry `json:"leaderboard"`
	TotalUsers  int                `json:"totalUsers"`
	LastUpdated string             `json:"lastUpdated"`
}

// LeaderboardManager handles leaderboard generation and caching.
type LeaderboardManager struct {
	d1     *api.D1Client
	r2     *api.R2Client
	logger *zap.Logger
}

// NewLeaderboardManager creates a new leaderboard manager.
func NewLeaderboardManager(d1Client *api.D1Client, r2Client *api.R2Client, logger *zap.Logger) *LeaderboardManager {
	return &LeaderboardManager{
		d1:     d1Client,
		r2:     r2Client,
		logger: logger,
	}
}

// GetLeaderboardData retrieves all users with their statistics for the leaderboard.
func (l *LeaderboardManager) GetLeaderboardData(ctx context.Context) ([]map[string]any, error) {
	sql := `
		SELECT
		  eu.discord_username,
		  eu.is_anonymous,
		  eu.total_points,
		  COALESCE(report_stats.total_reports, 0) as total_reports,
		  COALESCE(report_stats.confirmed_reports, 0) as confirmed_reports,
		  CASE
		    WHEN COALESCE(report_stats.total_reports, 0) = 0 THEN 0
		    ELSE ROUND((CAST(COALESCE(report_stats.confirmed_reports, 0) AS REAL) /
		                CAST(report_stats.total_reports AS REAL)) * 100, 2)
		  END as success_rate,
		  ROW_NUMBER() OVER (ORDER BY eu.total_points DESC) as rank
		FROM extension_users eu
		LEFT JOIN (
		  SELECT
		    extension_user_id,
		    COUNT(*) as total_reports,
		    SUM(CASE WHEN status = 'confirmed' THEN 1 ELSE 0 END) as confirmed_reports
		  FROM extension_reports
		  GROUP BY extension_user_id
		) report_stats ON eu.id = report_stats.extension_user_id
		ORDER BY eu.total_points DESC
	`

	results, err := l.d1.ExecuteSQL(ctx, sql, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get leaderboard data: %w", err)
	}

	return results, nil
}

// GetPublicLeaderboardData retrieves only non-anonymous users for the public leaderboard.
func (l *LeaderboardManager) GetPublicLeaderboardData(ctx context.Context) ([]map[string]any, error) {
	sql := `
		SELECT
		  eu.discord_username,
		  eu.is_anonymous,
		  eu.total_points,
		  COALESCE(report_stats.total_reports, 0) as total_reports,
		  COALESCE(report_stats.confirmed_reports, 0) as confirmed_reports,
		  CASE
		    WHEN COALESCE(report_stats.total_reports, 0) = 0 THEN 0
		    ELSE ROUND((CAST(COALESCE(report_stats.confirmed_reports, 0) AS REAL) /
		                CAST(report_stats.total_reports AS REAL)) * 100, 2)
		  END as success_rate,
		  ROW_NUMBER() OVER (ORDER BY eu.total_points DESC) as rank
		FROM extension_users eu
		LEFT JOIN (
		  SELECT
		    extension_user_id,
		    COUNT(*) as total_reports,
		    SUM(CASE WHEN status = 'confirmed' THEN 1 ELSE 0 END) as confirmed_reports
		  FROM extension_reports
		  GROUP BY extension_user_id
		) report_stats ON eu.id = report_stats.extension_user_id
		WHERE eu.is_anonymous = 0
		ORDER BY eu.total_points DESC
	`

	results, err := l.d1.ExecuteSQL(ctx, sql, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get public leaderboard data: %w", err)
	}

	return results, nil
}

// SaveLeaderboardToR2 generates and saves both leaderboard files to R2.
func (l *LeaderboardManager) SaveLeaderboardToR2(ctx context.Context) error {
	// Get all users leaderboard data
	allUsersResults, err := l.GetLeaderboardData(ctx)
	if err != nil {
		return fmt.Errorf("failed to get all users leaderboard data: %w", err)
	}

	allUsersData := l.buildLeaderboardData(allUsersResults)

	// Serialize all users data
	allUsersJSON, err := json.Marshal(allUsersData)
	if err != nil {
		return fmt.Errorf("failed to marshal all users leaderboard: %w", err)
	}

	// Save all users leaderboard
	allUsersKey := "extension/leaderboard/all-users.json"
	if err := l.r2.PutObject(ctx, allUsersKey, allUsersJSON, "application/json"); err != nil {
		return fmt.Errorf("failed to save all users leaderboard to R2: %w", err)
	}

	l.logger.Info("Saved all users leaderboard to R2",
		zap.String("key", allUsersKey),
		zap.Int("userCount", allUsersData.TotalUsers))

	// Get public only leaderboard data
	publicResults, err := l.GetPublicLeaderboardData(ctx)
	if err != nil {
		return fmt.Errorf("failed to get public leaderboard data: %w", err)
	}

	publicData := l.buildLeaderboardData(publicResults)

	// Serialize public data
	publicJSON, err := json.Marshal(publicData)
	if err != nil {
		return fmt.Errorf("failed to marshal public leaderboard: %w", err)
	}

	// Save public leaderboard
	publicKey := "extension/leaderboard/public-only.json"
	if err := l.r2.PutObject(ctx, publicKey, publicJSON, "application/json"); err != nil {
		return fmt.Errorf("failed to save public leaderboard to R2: %w", err)
	}

	l.logger.Info("Saved public leaderboard to R2",
		zap.String("key", publicKey),
		zap.Int("userCount", publicData.TotalUsers))

	return nil
}

// buildLeaderboardData processes query results into LeaderboardData structure.
func (l *LeaderboardManager) buildLeaderboardData(results []map[string]any) *LeaderboardData {
	entries := make([]LeaderboardEntry, 0, len(results))

	for _, row := range results {
		rank := int(row["rank"].(float64))
		isAnonymous := row["is_anonymous"].(float64) == 1

		// Generate display name based on anonymous status
		var displayName string
		if isAnonymous {
			displayName = fmt.Sprintf("Anonymous Hunter #%d", rank)
		} else {
			displayName = row["discord_username"].(string)
		}

		entry := LeaderboardEntry{
			Rank:         rank,
			DisplayName:  displayName,
			TotalPoints:  int(row["total_points"].(float64)),
			TotalReports: int(row["total_reports"].(float64)),
			SuccessRate:  row["success_rate"].(float64),
			IsAnonymous:  isAnonymous,
		}
		entries = append(entries, entry)
	}

	return &LeaderboardData{
		Leaderboard: entries,
		TotalUsers:  len(entries),
		LastUpdated: time.Now().UTC().Format(time.RFC3339),
	}
}

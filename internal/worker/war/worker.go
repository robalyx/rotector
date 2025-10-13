package war

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/robalyx/rotector/internal/cloudflare/manager"
	"github.com/robalyx/rotector/internal/database"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/database/types/enum"
	"github.com/robalyx/rotector/internal/roblox/fetcher"
	"github.com/robalyx/rotector/internal/setup"
	"github.com/robalyx/rotector/internal/tui/components"
	"github.com/robalyx/rotector/internal/worker/core"
	"github.com/robalyx/rotector/pkg/utils"
	"go.uber.org/zap"
)

// ZoneStats represents user statistics for a zone.
type ZoneStats struct {
	TotalUsers     int64
	BannedUsers    int64
	FlaggedUsers   int64
	ConfirmedUsers int64
}

// Worker handles war map management and target selection.
type Worker struct {
	db                    database.Client
	bar                   *components.ProgressBar
	reporter              *core.StatusReporter
	warData               *manager.WarData
	warManager            *manager.WarManager
	warStats              *manager.WarStats
	leaderboardManager    *manager.LeaderboardManager
	userFetcher           *fetcher.UserFetcher
	logger                *zap.Logger
	lastDailyStats        time.Time
	lastLeaderboardUpdate time.Time
}

// New creates a new war worker.
func New(app *setup.App, bar *components.ProgressBar, logger *zap.Logger, instanceID string) *Worker {
	userFetcher := fetcher.NewUserFetcher(app, logger)
	reporter := core.NewStatusReporter(app.StatusClient, "war", instanceID, logger)

	return &Worker{
		db:                 app.DB,
		bar:                bar,
		reporter:           reporter,
		warData:            app.CFClient.WarData,
		warManager:         app.CFClient.WarManager,
		warStats:           manager.NewWarStats(app.CFClient.GetD1Client(), logger),
		leaderboardManager: app.CFClient.Leaderboard,
		userFetcher:        userFetcher,
		logger:             logger.Named("war_worker"),
	}
}

// Start begins the war worker's main loop.
func (w *Worker) Start(ctx context.Context) {
	w.logger.Info("War Worker started", zap.String("workerID", w.reporter.GetWorkerID()))

	w.reporter.Start(ctx)
	defer w.reporter.Stop()

	w.bar.SetTotal(100)
	w.bar.Reset()
	w.reporter.SetHealthy(true)

	if err := w.updateWarState(ctx); err != nil {
		w.logger.Error("Failed to update war state", zap.Error(err))
		w.reporter.SetHealthy(false)
	}

	ticker := time.NewTicker(5 * time.Minute) // Check every 5 minutes
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.bar.SetStepMessage("Shutting down", 100)
			w.reporter.UpdateStatus("Shutting down", 100)

			return

		case <-ticker.C:
			w.bar.Reset()
			w.reporter.SetHealthy(true)

			if err := w.updateWarState(ctx); err != nil {
				w.logger.Error("Failed to update war state", zap.Error(err))
				w.reporter.SetHealthy(false)

				if !utils.ErrorSleep(ctx, 30*time.Second, w.logger, "war worker") {
					return
				}
			}
		}
	}
}

// updateWarState performs the main war worker cycle.
func (w *Worker) updateWarState(ctx context.Context) error {
	// Step 1: Clean up expired targets (20%)
	w.bar.SetStepMessage("Cleaning expired targets", 20)
	w.reporter.UpdateStatus("Cleaning expired targets", 20)

	if err := w.cleanupExpiredTargets(ctx); err != nil {
		return fmt.Errorf("failed to cleanup expired targets: %w", err)
	}

	// Step 2: Check ban status of active targets (35%)
	w.bar.SetStepMessage("Checking target ban status", 35)
	w.reporter.UpdateStatus("Checking target ban status", 35)

	if err := w.checkTargetBanStatus(ctx); err != nil {
		return fmt.Errorf("failed to check ban status: %w", err)
	}

	// Step 3: Maintain 6 global targets (50%)
	w.bar.SetStepMessage("Maintaining global targets", 50)
	w.reporter.UpdateStatus("Maintaining global targets", 50)

	w.maintainGlobalTargets(ctx)

	// Step 4: Update zone statistics (65%)
	w.bar.SetStepMessage("Updating zone statistics", 65)
	w.reporter.UpdateStatus("Updating zone statistics", 65)

	w.updateZoneStatistics(ctx)

	// Step 5: Process extension reports (80%)
	w.bar.SetStepMessage("Processing extension reports", 80)
	w.reporter.UpdateStatus("Processing extension reports", 80)

	if err := w.processExtensionReports(ctx); err != nil {
		return fmt.Errorf("failed to process extension reports: %w", err)
	}

	// Step 6: Record daily statistics if 24 hours have passed (90%)
	w.bar.SetStepMessage("Recording daily statistics", 90)
	w.reporter.UpdateStatus("Recording daily statistics", 90)

	if time.Since(w.lastDailyStats) >= 24*time.Hour {
		w.recordDailyStatistics(ctx)
		w.lastDailyStats = time.Now()
	}

	// Step 7: Update leaderboard cache if needed (95%)
	w.bar.SetStepMessage("Checking leaderboard cache", 95)
	w.reporter.UpdateStatus("Checking leaderboard cache", 95)

	w.updateLeaderboardCache(ctx)

	// Step 8: Save war map data to R2 (97%)
	w.bar.SetStepMessage("Saving war map data", 97)
	w.reporter.UpdateStatus("Saving war map data", 97)

	if err := w.warData.SaveWarMapData(ctx); err != nil {
		return fmt.Errorf("failed to save war map data: %w", err)
	}

	// Step 9: Save zone details to R2 (100%)
	w.bar.SetStepMessage("Saving zone details", 100)
	w.reporter.UpdateStatus("Saving zone details", 100)

	for zoneID := range int64(7) {
		zoneDetails, err := w.warData.GetZoneDetailsData(ctx, zoneID)
		if err != nil {
			w.logger.Warn("Failed to get zone details",
				zap.Int64("zoneID", zoneID), zap.Error(err))

			continue
		}

		if err := w.warData.SaveZoneDetailsData(ctx, zoneID, zoneDetails); err != nil {
			w.logger.Warn("Failed to save zone details",
				zap.Int64("zoneID", zoneID), zap.Error(err))
		}
	}

	w.logger.Info("War state update completed successfully")

	return nil
}

// cleanupExpiredTargets removes targets that have been active for more than 24 hours.
func (w *Worker) cleanupExpiredTargets(ctx context.Context) error {
	expiredTargets, err := w.warManager.GetExpiredTargets(ctx)
	if err != nil {
		return err
	}

	for _, userID := range expiredTargets {
		if err := w.warManager.RemoveActiveTarget(ctx, userID, "expired_24h"); err != nil {
			w.logger.Warn("Failed to remove expired target",
				zap.Int64("userID", userID), zap.Error(err))
		}
	}

	if len(expiredTargets) > 0 {
		w.logger.Info("Cleaned up expired targets", zap.Int("count", len(expiredTargets)))
	}

	return nil
}

// checkTargetBanStatus checks if active targets have been banned using Roblox API.
func (w *Worker) checkTargetBanStatus(ctx context.Context) error {
	// Get list of all users currently marked as active targets
	targetUserIDs, err := w.warManager.GetActiveTargetUserIDs(ctx)
	if err != nil {
		return fmt.Errorf("failed to get active target user IDs: %w", err)
	}

	if len(targetUserIDs) == 0 {
		return nil
	}

	// Check ban status using Roblox API
	bannedUserIDs, err := w.userFetcher.FetchBannedUsers(ctx, targetUserIDs)
	if err != nil {
		return fmt.Errorf("failed to check banned users: %w", err)
	}

	// Remove banned targets and update database
	bannedCount := 0

	for _, bannedUserID := range bannedUserIDs {
		// Remove from active targets
		if err := w.warManager.RemoveActiveTarget(ctx, bannedUserID, "banned"); err != nil {
			w.logger.Warn("Failed to remove banned target",
				zap.Int64("userID", bannedUserID), zap.Error(err))

			continue
		}

		// Mark user as banned in database
		if err := w.db.Model().User().MarkUsersBanStatus(ctx, []int64{bannedUserID}, true); err != nil {
			w.logger.Warn("Failed to mark user as banned in database",
				zap.Int64("userID", bannedUserID), zap.Error(err))
		}

		bannedCount++
	}

	if bannedCount > 0 {
		w.logger.Info("Removed banned targets", zap.Int("count", bannedCount))
	}

	return nil
}

// maintainGlobalTargets ensures there are exactly 6 active global targets.
func (w *Worker) maintainGlobalTargets(ctx context.Context) {
	currentTargets, err := w.warManager.GetGlobalTargetCount(ctx)
	if err != nil {
		w.logger.Warn("Failed to get global target count", zap.Error(err))
		return
	}

	needed := 6 - currentTargets

	if needed <= 0 {
		return // Already have enough targets
	}

	// Get candidate targets
	candidates, err := w.getCandidateTargetsFromDB(ctx, needed)
	if err != nil {
		w.logger.Warn("Failed to get global candidates", zap.Error(err))
		return
	}

	// Add candidates as active targets
	added := 0

	for _, candidate := range candidates {
		err := w.warManager.AddActiveTarget(ctx,
			candidate.UserID, candidate.ZoneID, candidate.UserName, candidate.UserStatus, candidate.Confidence)
		if err != nil {
			w.logger.Warn("Failed to add candidate as target",
				zap.Int64("userID", candidate.UserID), zap.Error(err))

			continue
		}

		added++
	}

	if added > 0 {
		w.logger.Info("Added new global targets",
			zap.Int("added", added),
			zap.Int("total", currentTargets+added))
	}
}

// updateZoneStatistics updates user counts and liberation percentages for all zones.
func (w *Worker) updateZoneStatistics(ctx context.Context) {
	for zoneID := range int64(7) {
		stats, err := w.calculateZoneStats(ctx, zoneID)
		if err != nil {
			w.logger.Warn("Failed to calculate zone stats",
				zap.Int64("zoneID", zoneID), zap.Error(err))

			continue
		}

		err = w.warManager.UpdateZoneStats(ctx, zoneID,
			stats.TotalUsers, stats.BannedUsers, stats.FlaggedUsers, stats.ConfirmedUsers)
		if err != nil {
			w.logger.Warn("Failed to update zone stats",
				zap.Int64("zoneID", zoneID), zap.Error(err))
		}
	}

	// Update major order progress
	if err := w.warManager.UpdateMajorOrderProgress(ctx); err != nil {
		w.logger.Warn("Failed to update major order progress", zap.Error(err))
	}
}

// getCandidateTargetsFromDB finds eligible users for global targeting with 7-day cooldown.
func (w *Worker) getCandidateTargetsFromDB(ctx context.Context, limit int) ([]manager.CandidateTarget, error) {
	// Get user IDs on cooldown (active targets from last 7 days)
	excludeUserIDs, err := w.warManager.GetRecentTargetUserIDs(ctx, 7)
	if err != nil {
		w.logger.Warn("Failed to get recent target IDs, proceeding without cooldown filter", zap.Error(err))

		excludeUserIDs = []int64{}
	}

	// Get candidates from PostgreSQL
	candidates, err := w.db.Model().User().GetGlobalTargetCandidates(ctx, limit*2, excludeUserIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to get global target candidates from PostgreSQL: %w", err)
	}

	if len(candidates) == 0 {
		return []manager.CandidateTarget{}, nil
	}

	// Extract user IDs for ban checking
	candidateIDs := make([]int64, len(candidates))
	for i, candidate := range candidates {
		candidateIDs[i] = candidate.UserID
	}

	// Check ban status using Roblox API
	bannedIDs, err := w.userFetcher.FetchBannedUsers(ctx, candidateIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to check ban status: %w", err)
	}

	// Mark banned users in database
	if len(bannedIDs) > 0 {
		if err := w.db.Model().User().MarkUsersBanStatus(ctx, bannedIDs, true); err != nil {
			w.logger.Warn("Failed to mark users as banned", zap.Error(err))
		}
	}

	// Filter out banned users and convert to proper format
	bannedSet := make(map[int64]bool)
	for _, id := range bannedIDs {
		bannedSet[id] = true
	}

	finalCandidates := make([]manager.CandidateTarget, 0, limit)
	for _, candidate := range candidates {
		if !bannedSet[candidate.UserID] && len(finalCandidates) < limit {
			finalCandidates = append(finalCandidates, manager.CandidateTarget{
				UserID:     candidate.UserID,
				UserName:   candidate.Name,
				UserStatus: int(candidate.Status),
				Confidence: candidate.Confidence,
				ZoneID:     int64(candidate.Category),
			})
		}
	}

	if len(bannedIDs) > 0 {
		w.logger.Info("Filtered out banned candidates",
			zap.Int("bannedCount", len(bannedIDs)))
	}

	w.logger.Debug("Selected candidates for global targeting",
		zap.Int("candidateCount", len(finalCandidates)),
		zap.Int("onCooldown", len(excludeUserIDs)))

	return finalCandidates, nil
}

// calculateZoneStats calculates user statistics for a zone from PostgreSQL.
func (w *Worker) calculateZoneStats(ctx context.Context, zoneID int64) (ZoneStats, error) {
	// Get user IDs for this zone
	userIDs, err := w.db.Model().User().GetUserIDsByCategory(ctx, enum.UserCategoryType(zoneID))
	if err != nil {
		return ZoneStats{}, err
	}

	if len(userIDs) == 0 {
		return ZoneStats{
			TotalUsers:     0,
			BannedUsers:    0,
			FlaggedUsers:   0,
			ConfirmedUsers: 0,
		}, nil
	}

	// Get user details
	userMap, err := w.db.Model().User().GetUsersByIDs(ctx, userIDs,
		types.UserFieldBasic|types.UserFieldProfile|types.UserFieldStats)
	if err != nil {
		return ZoneStats{}, err
	}

	// Count users by status
	var totalUsers, bannedUsers, flaggedUsers, confirmedUsers int64

	for _, user := range userMap {
		if user.Status == enum.UserTypeCleared {
			continue
		}

		totalUsers++

		if user.IsBanned {
			bannedUsers++
		}

		switch user.Status {
		case enum.UserTypeFlagged:
			flaggedUsers++
		case enum.UserTypeConfirmed:
			confirmedUsers++
		case enum.UserTypeCleared:
			// This case should never be reached due to continue above
		case enum.UserTypeQueued, enum.UserTypeBloxDB, enum.UserTypeMixed, enum.UserTypePastOffender:
			// These statuses are not counted in war zone statistics
		}
	}

	stats := ZoneStats{
		TotalUsers:     totalUsers,
		BannedUsers:    bannedUsers,
		FlaggedUsers:   flaggedUsers,
		ConfirmedUsers: confirmedUsers,
	}

	return stats, nil
}

// processExtensionReports processes pending extension reports and awards points.
func (w *Worker) processExtensionReports(ctx context.Context) error {
	reports, err := w.warManager.GetPendingExtensionReports(ctx)
	if err != nil {
		return fmt.Errorf("failed to get pending extension reports: %w", err)
	}

	if len(reports) == 0 {
		return nil
	}

	processedCount := 0

	for _, report := range reports {
		if err := w.processExtensionReport(ctx, report); err != nil {
			w.logger.Warn("Failed to process extension report",
				zap.Int64("reportID", report.ID),
				zap.Error(err))

			continue
		}

		processedCount++
	}

	if processedCount > 0 {
		w.logger.Info("Processed extension reports", zap.Int("count", processedCount))
	}

	return nil
}

// processExtensionReport processes a single extension report.
// Awards points only if the user has been banned by Roblox after the report was submitted.
func (w *Worker) processExtensionReport(ctx context.Context, report manager.ExtensionReport) error {
	// Get user details including ban status and timestamps
	userMap, err := w.db.Model().User().GetUsersByIDs(ctx, []int64{report.ReportedUserID},
		types.UserFieldBasic|types.UserFieldProfile|types.UserFieldTimestamps)
	if err != nil {
		return fmt.Errorf("failed to get user details: %w", err)
	}

	user, exists := userMap[report.ReportedUserID]
	if !exists {
		// User not in our system, reject the report
		if err := w.warManager.UpdateExtensionReport(ctx, report.ID, "rejected", 0); err != nil {
			return fmt.Errorf("failed to update extension report: %w", err)
		}

		return nil
	}

	// Check if user is flagged or confirmed (not cleared)
	if user.Status != enum.UserTypeFlagged && user.Status != enum.UserTypeConfirmed {
		// Not a flagged user, reject
		if err := w.warManager.UpdateExtensionReport(ctx, report.ID, "rejected", 0); err != nil {
			return fmt.Errorf("failed to update extension report: %w", err)
		}

		return nil
	}

	// Parse report timestamp
	reportedAt, err := time.Parse("2006-01-02 15:04:05", report.ReportedAt)
	if err != nil {
		w.logger.Warn("Failed to parse report timestamp",
			zap.String("reportedAt", report.ReportedAt),
			zap.Error(err))

		return fmt.Errorf("failed to parse report timestamp: %w", err)
	}

	var (
		status        string
		pointsAwarded int
	)

	// Award points only if user is banned AND ban was detected after the report
	if user.IsBanned && user.LastBanCheck.After(reportedAt) {
		status = "confirmed"
		pointsAwarded = 10 // Base points for successful report leading to ban

		// Award extra points if this user was first to report
		isFirstReporter, err := w.warManager.IsFirstReporter(ctx, report.ReportedUserID, report.ExtensionUserUUID)
		if err != nil {
			w.logger.Warn("Failed to check first reporter status",
				zap.Int64("userID", report.ReportedUserID),
				zap.Error(err))
		} else if isFirstReporter {
			pointsAwarded += 5 // First reporter bonus
		}
	} else {
		status = "rejected"
		pointsAwarded = 0
	}

	// Update the report status in the database
	if err := w.warManager.UpdateExtensionReport(ctx, report.ID, status, pointsAwarded); err != nil {
		return fmt.Errorf("failed to update extension report: %w", err)
	}

	// Credit points to the reporter's account
	if pointsAwarded > 0 {
		if err := w.warManager.AwardPointsToExtensionUser(ctx, report.ExtensionUserUUID, pointsAwarded); err != nil {
			w.logger.Warn("Failed to award points to extension user",
				zap.String("uuid", report.ExtensionUserUUID),
				zap.Int("points", pointsAwarded),
				zap.Error(err))
		}
	}

	w.logger.Debug("Processed extension report",
		zap.Int64("reportID", report.ID),
		zap.Int64("reportedUserID", report.ReportedUserID),
		zap.String("status", status),
		zap.Int("pointsAwarded", pointsAwarded),
		zap.Bool("isBanned", user.IsBanned),
		zap.Time("reportedAt", reportedAt),
		zap.Time("lastBanCheck", user.LastBanCheck))

	return nil
}

// recordDailyStatistics records daily zone and global statistics per war-stats-worker-spec.
// Records 40 statistics total: 35 zone stats (5 per zone × 7 zones) + 5 global stats.
func (w *Worker) recordDailyStatistics(ctx context.Context) {
	var (
		zoneStats       = make([]ZoneStats, 0, 7)
		zoneLiberations = make([]float64, 0, 7)
		records         = make([]manager.StatisticRecord, 0, 40)
	)

	// Calculate zone statistics (5 stats per zone)
	for zoneID := range int64(7) {
		stats, err := w.calculateZoneStats(ctx, zoneID)
		if err != nil {
			w.logger.Warn("Failed to calculate zone stats for daily recording",
				zap.Int64("zoneID", zoneID), zap.Error(err))

			continue
		}

		zoneStats = append(zoneStats, stats)

		// Calculate liberation percentage
		liberation := 0.0
		if stats.TotalUsers > 0 {
			liberation = float64(stats.BannedUsers) / float64(stats.TotalUsers) * 100
		}

		zoneLiberations = append(zoneLiberations, liberation)

		zoneKey := strconv.FormatInt(zoneID, 10)

		// Collect zone statistics
		records = append(records,
			manager.StatisticRecord{StatType: "zone_liberation", StatKey: zoneKey, StatValue: liberation},
			manager.StatisticRecord{StatType: "zone_total_users", StatKey: zoneKey, StatValue: float64(stats.TotalUsers)},
			manager.StatisticRecord{StatType: "zone_banned_users", StatKey: zoneKey, StatValue: float64(stats.BannedUsers)},
			manager.StatisticRecord{StatType: "zone_flagged_users", StatKey: zoneKey, StatValue: float64(stats.FlaggedUsers)},
			manager.StatisticRecord{StatType: "zone_confirmed_users", StatKey: zoneKey, StatValue: float64(stats.ConfirmedUsers)},
		)
	}

	// Calculate global statistics (5 stats)
	var globalTotalUsers, globalBannedUsers, globalFlaggedUsers, globalConfirmedUsers int64
	for _, stats := range zoneStats {
		globalTotalUsers += stats.TotalUsers
		globalBannedUsers += stats.BannedUsers
		globalFlaggedUsers += stats.FlaggedUsers
		globalConfirmedUsers += stats.ConfirmedUsers
	}

	// Calculate average liberation across zones
	globalLiberation := 0.0

	if len(zoneLiberations) > 0 {
		sum := 0.0
		for _, lib := range zoneLiberations {
			sum += lib
		}

		globalLiberation = sum / float64(len(zoneLiberations))
	}

	// Collect global statistics
	records = append(records,
		manager.StatisticRecord{StatType: "global_liberation", StatKey: "global", StatValue: globalLiberation},
		manager.StatisticRecord{StatType: "global_total_users", StatKey: "global", StatValue: float64(globalTotalUsers)},
		manager.StatisticRecord{StatType: "global_banned_users", StatKey: "global", StatValue: float64(globalBannedUsers)},
		manager.StatisticRecord{StatType: "global_flagged_users", StatKey: "global", StatValue: float64(globalFlaggedUsers)},
		manager.StatisticRecord{StatType: "global_confirmed_users", StatKey: "global", StatValue: float64(globalConfirmedUsers)},
	)

	// Record all statistics
	if err := w.warStats.RecordStatisticsBatch(ctx, records); err != nil {
		w.logger.Error("Failed to record daily statistics batch", zap.Error(err))
		return
	}

	w.logger.Info("Recorded daily statistics",
		zap.Int("totalRecords", len(records)),
		zap.Int("zones", len(zoneStats)),
		zap.Int64("globalTotalUsers", globalTotalUsers),
		zap.Float64("globalLiberation", globalLiberation))
}

// updateLeaderboardCache updates the leaderboard cache if 6 hours have passed.
func (w *Worker) updateLeaderboardCache(ctx context.Context) {
	now := time.Now()

	// Update leaderboard every 6 hours
	if now.Sub(w.lastLeaderboardUpdate) < 6*time.Hour {
		w.logger.Debug("Skipping leaderboard update, not enough time has passed",
			zap.Duration("timeSinceLastUpdate", now.Sub(w.lastLeaderboardUpdate)))

		return
	}

	// Update leaderboard cache
	if err := w.leaderboardManager.SaveLeaderboardToR2(ctx); err != nil {
		w.logger.Error("Failed to update leaderboard cache", zap.Error(err))

		return
	}

	w.lastLeaderboardUpdate = now
	w.logger.Info("Successfully updated leaderboard cache")
}

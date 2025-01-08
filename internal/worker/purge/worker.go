package purge

import (
	"context"
	"time"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/rotector/rotector/internal/common/client/checker"
	"github.com/rotector/rotector/internal/common/client/fetcher"
	"github.com/rotector/rotector/internal/common/progress"
	"github.com/rotector/rotector/internal/common/setup"
	"github.com/rotector/rotector/internal/common/storage/database"
	"github.com/rotector/rotector/internal/worker/core"
	"go.uber.org/zap"
)

// Worker handles all purge operations.
type Worker struct {
	db                 *database.Client
	roAPI              *api.API
	bar                *progress.Bar
	userFetcher        *fetcher.UserFetcher
	groupFetcher       *fetcher.GroupFetcher
	thumbnailFetcher   *fetcher.ThumbnailFetcher
	groupChecker       *checker.GroupChecker
	reporter           *core.StatusReporter
	logger             *zap.Logger
	userBatchSize      int
	groupBatchSize     int
	trackBatchSize     int
	minFlaggedOverride int
	minFlaggedPercent  float64
}

// New creates a new purge worker.
func New(app *setup.App, bar *progress.Bar, logger *zap.Logger) *Worker {
	userFetcher := fetcher.NewUserFetcher(app, logger)
	groupFetcher := fetcher.NewGroupFetcher(app.RoAPI, logger)
	thumbnailFetcher := fetcher.NewThumbnailFetcher(app.RoAPI, logger)
	reporter := core.NewStatusReporter(app.StatusClient, "purge", "main", logger)
	groupChecker := checker.NewGroupChecker(
		app.DB,
		logger,
		app.Config.Worker.ThresholdLimits.MaxGroupMembersTrack,
		app.Config.Worker.ThresholdLimits.MinFlaggedOverride,
		app.Config.Worker.ThresholdLimits.MinFlaggedPercentage,
	)

	return &Worker{
		db:                 app.DB,
		roAPI:              app.RoAPI,
		bar:                bar,
		userFetcher:        userFetcher,
		groupFetcher:       groupFetcher,
		thumbnailFetcher:   thumbnailFetcher,
		groupChecker:       groupChecker,
		reporter:           reporter,
		logger:             logger,
		userBatchSize:      app.Config.Worker.BatchSizes.PurgeUsers,
		groupBatchSize:     app.Config.Worker.BatchSizes.PurgeGroups,
		trackBatchSize:     app.Config.Worker.BatchSizes.TrackGroups,
		minFlaggedOverride: app.Config.Worker.ThresholdLimits.MinFlaggedOverride,
		minFlaggedPercent:  app.Config.Worker.ThresholdLimits.MinFlaggedPercentage,
	}
}

// Start begins the purge worker's main loop.
func (w *Worker) Start() {
	w.logger.Info("Purge Worker started", zap.String("workerID", w.reporter.GetWorkerID()))
	w.reporter.Start()
	defer w.reporter.Stop()

	w.bar.SetTotal(100)

	for {
		w.bar.Reset()
		w.reporter.SetHealthy(true)

		// Step 1: Process banned users (25%)
		w.processBannedUsers()

		// Step 2: Process locked groups (35%)
		w.processLockedGroups()

		// Step 3: Process cleared users (50%)
		w.processClearedUsers()

		// Step 4: Process cleared groups (65%)
		w.processClearedGroups()

		// Step 5: Process group tracking (85%)
		w.processGroupTracking()

		// Step 6: Completed (100%)
		w.bar.SetStepMessage("Completed", 100)
		w.reporter.UpdateStatus("Completed", 100)

		// Wait before next cycle
		time.Sleep(1 * time.Minute)
	}
}

// processBannedUsers checks for and removes banned users.
func (w *Worker) processBannedUsers() {
	w.bar.SetStepMessage("Processing banned users", 25)
	w.reporter.UpdateStatus("Processing banned users", 25)

	// Get users to check
	users, err := w.db.Users().GetUsersToCheck(context.Background(), w.userBatchSize)
	if err != nil {
		w.logger.Error("Error getting users to check", zap.Error(err))
		w.reporter.SetHealthy(false)
		return
	}

	if len(users) == 0 {
		w.logger.Info("No users to check for bans")
		return
	}

	// Check for banned users
	bannedUserIDs, err := w.userFetcher.FetchBannedUsers(users)
	if err != nil {
		w.logger.Error("Error fetching banned users", zap.Error(err))
		w.reporter.SetHealthy(false)
		return
	}

	// Remove banned users
	if len(bannedUserIDs) > 0 {
		err = w.db.Users().RemoveBannedUsers(context.Background(), bannedUserIDs)
		if err != nil {
			w.logger.Error("Error removing banned users", zap.Error(err))
			w.reporter.SetHealthy(false)
			return
		}
		w.logger.Info("Removed banned users", zap.Int("count", len(bannedUserIDs)))
	}
}

// processLockedGroups checks for and removes locked groups.
func (w *Worker) processLockedGroups() {
	w.bar.SetStepMessage("Processing locked groups", 35)
	w.reporter.UpdateStatus("Processing locked groups", 35)

	// Get groups to check
	groups, err := w.db.Groups().GetGroupsToCheck(context.Background(), w.groupBatchSize)
	if err != nil {
		w.logger.Error("Error getting groups to check", zap.Error(err))
		w.reporter.SetHealthy(false)
		return
	}

	if len(groups) == 0 {
		w.logger.Info("No groups to check for locks")
		return
	}

	// Check for locked groups
	lockedGroupIDs, err := w.groupFetcher.FetchLockedGroups(groups)
	if err != nil {
		w.logger.Error("Error fetching locked groups", zap.Error(err))
		w.reporter.SetHealthy(false)
		return
	}

	// Remove locked groups
	if len(lockedGroupIDs) > 0 {
		err = w.db.Groups().RemoveLockedGroups(context.Background(), lockedGroupIDs)
		if err != nil {
			w.logger.Error("Error removing locked groups", zap.Error(err))
			w.reporter.SetHealthy(false)
			return
		}
		w.logger.Info("Removed locked groups", zap.Int("count", len(lockedGroupIDs)))
	}
}

// processClearedUsers removes old cleared users.
func (w *Worker) processClearedUsers() {
	w.bar.SetStepMessage("Processing cleared users", 50)
	w.reporter.UpdateStatus("Processing cleared users", 50)

	cutoffDate := time.Now().AddDate(0, 0, -30) // 30 days ago
	affected, err := w.db.Users().PurgeOldClearedUsers(context.Background(), cutoffDate)
	if err != nil {
		w.logger.Error("Error purging old cleared users", zap.Error(err))
		w.reporter.SetHealthy(false)
		return
	}

	if affected > 0 {
		w.logger.Info("Purged old cleared users",
			zap.Int("affected", affected),
			zap.Time("cutoffDate", cutoffDate))
	}
}

// processClearedGroups removes old cleared groups.
func (w *Worker) processClearedGroups() {
	w.bar.SetStepMessage("Processing cleared groups", 65)
	w.reporter.UpdateStatus("Processing cleared groups", 65)

	cutoffDate := time.Now().AddDate(0, 0, -30) // 30 days ago
	affected, err := w.db.Groups().PurgeOldClearedGroups(context.Background(), cutoffDate)
	if err != nil {
		w.logger.Error("Error purging old cleared groups", zap.Error(err))
		w.reporter.SetHealthy(false)
		return
	}

	if affected > 0 {
		w.logger.Info("Purged old cleared groups",
			zap.Int("affected", affected),
			zap.Time("cutoffDate", cutoffDate))
	}
}

// processGroupTracking manages group tracking data.
func (w *Worker) processGroupTracking() {
	w.bar.SetStepMessage("Processing group tracking", 75)
	w.reporter.UpdateStatus("Processing group tracking", 75)

	// Get groups to check
	groupsWithUsers, err := w.db.Tracking().GetGroupTrackingsToCheck(context.Background(), w.trackBatchSize)
	if err != nil {
		w.logger.Error("Error checking group trackings", zap.Error(err))
		w.reporter.SetHealthy(false)
		return
	}

	// Check if there are any groups to check
	if len(groupsWithUsers) == 0 {
		w.logger.Info("No groups to check for tracking")
		return
	}

	// Extract group IDs for batch lookup
	groupIDs := make([]uint64, 0, len(groupsWithUsers))
	for groupID := range groupsWithUsers {
		groupIDs = append(groupIDs, groupID)
	}

	// Load group information from API
	groupInfos := w.groupFetcher.FetchGroupInfos(groupIDs)
	if len(groupInfos) == 0 {
		return
	}

	// Check which groups exceed the percentage threshold
	flaggedGroups := w.groupChecker.CheckGroupPercentages(groupInfos, groupsWithUsers)
	if len(flaggedGroups) == 0 {
		return
	}

	// Add thumbnails to flagged groups
	flaggedGroups = w.thumbnailFetcher.AddGroupImageURLs(flaggedGroups)

	// Save flagged groups to database
	if err := w.db.Groups().SaveGroups(context.Background(), flaggedGroups); err != nil {
		w.logger.Error("Failed to save flagged groups", zap.Error(err))
		return
	}

	// Extract group IDs that were flagged
	flaggedGroupIDs := make([]uint64, 0, len(flaggedGroups))
	for _, group := range flaggedGroups {
		flaggedGroupIDs = append(flaggedGroupIDs, group.ID)
	}

	// Update tracking entries to mark them as flagged
	if err := w.db.Tracking().UpdateFlaggedGroups(context.Background(), flaggedGroupIDs); err != nil {
		w.logger.Error("Failed to update tracking entries", zap.Error(err))
		return
	}

	w.logger.Info("Processed group trackings",
		zap.Int("checkedGroups", len(groupInfos)),
		zap.Int("flaggedGroups", len(flaggedGroups)))
}

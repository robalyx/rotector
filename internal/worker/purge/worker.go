package purge

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/rotector/rotector/internal/common/client/fetcher"
	"github.com/rotector/rotector/internal/common/progress"
	"github.com/rotector/rotector/internal/common/setup"
	"github.com/rotector/rotector/internal/common/storage/database"
	"github.com/rotector/rotector/internal/common/storage/database/types"
	"github.com/rotector/rotector/internal/worker/core"
	"go.uber.org/zap"
)

// Worker handles all purge operations.
type Worker struct {
	db               *database.Client
	roAPI            *api.API
	bar              *progress.Bar
	userFetcher      *fetcher.UserFetcher
	groupFetcher     *fetcher.GroupFetcher
	thumbnailFetcher *fetcher.ThumbnailFetcher
	reporter         *core.StatusReporter
	logger           *zap.Logger
	userBatchSize    int
	groupBatchSize   int
	minFlaggedUsers  int
}

// New creates a new purge worker.
func New(app *setup.App, bar *progress.Bar, logger *zap.Logger) *Worker {
	userFetcher := fetcher.NewUserFetcher(app, logger)
	groupFetcher := fetcher.NewGroupFetcher(app.RoAPI, logger)
	thumbnailFetcher := fetcher.NewThumbnailFetcher(app.RoAPI, logger)
	reporter := core.NewStatusReporter(app.StatusClient, "purge", "main", logger)

	return &Worker{
		db:               app.DB,
		roAPI:            app.RoAPI,
		bar:              bar,
		userFetcher:      userFetcher,
		groupFetcher:     groupFetcher,
		thumbnailFetcher: thumbnailFetcher,
		reporter:         reporter,
		logger:           logger,
		userBatchSize:    app.Config.Worker.BatchSizes.PurgeUsers,
		groupBatchSize:   app.Config.Worker.BatchSizes.PurgeGroups,
		minFlaggedUsers:  app.Config.Worker.ThresholdLimits.MinFlaggedForGroup,
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
		time.Sleep(5 * time.Minute)
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

	// Check group trackings
	if err := w.checkGroupTrackings(); err != nil {
		w.logger.Error("Error checking group trackings", zap.Error(err))
		w.reporter.SetHealthy(false)
		return
	}

	// Purge old trackings
	cutoffDate := time.Now().AddDate(0, 0, -30)
	affected, err := w.db.Tracking().PurgeOldTrackings(context.Background(), cutoffDate)
	if err != nil {
		w.logger.Error("Error purging old trackings", zap.Error(err))
		w.reporter.SetHealthy(false)
		return
	}

	if affected > 0 {
		w.logger.Info("Purged old trackings",
			zap.Int("affected", affected),
			zap.Time("cutoffDate", cutoffDate))
	}
}

// checkGroupTrackings analyzes group member lists to find groups with many
// flagged users. Groups exceeding the threshold are flagged with a confidence
// score based on the ratio of flagged members.
func (w *Worker) checkGroupTrackings() error {
	groupsWithUsers, err := w.db.Tracking().GetAndRemoveQualifiedGroupTrackings(context.Background(), w.minFlaggedUsers)
	if err != nil {
		return err
	}

	// Extract group IDs for batch lookup
	groupIDs := make([]uint64, 0, len(groupsWithUsers))
	for groupID := range groupsWithUsers {
		groupIDs = append(groupIDs, groupID)
	}

	// Load group information from API
	groupInfos := w.groupFetcher.FetchGroupInfos(groupIDs)
	if len(groupInfos) == 0 {
		return nil
	}

	// Create flagged group entries with confidence scores
	flaggedGroups := make([]*types.FlaggedGroup, 0, len(groupInfos))
	for _, groupInfo := range groupInfos {
		flaggedUsers := groupsWithUsers[groupInfo.ID]
		flaggedGroups = append(flaggedGroups, &types.FlaggedGroup{
			Group: types.Group{
				ID:           groupInfo.ID,
				Name:         groupInfo.Name,
				Description:  groupInfo.Description,
				Owner:        groupInfo.Owner.UserID,
				Shout:        groupInfo.Shout,
				MemberCount:  groupInfo.MemberCount,
				Reason:       fmt.Sprintf("Group has at least %d flagged users", w.minFlaggedUsers),
				Confidence:   math.Min(float64(len(flaggedUsers))/(float64(w.minFlaggedUsers)*10), 1.0),
				LastUpdated:  time.Now(),
				FlaggedUsers: flaggedUsers,
			},
		})
	}

	// Add thumbnails and save to database
	flaggedGroups = w.thumbnailFetcher.AddGroupImageURLs(flaggedGroups)
	if err := w.db.Groups().SaveFlaggedGroups(context.Background(), flaggedGroups); err != nil {
		return fmt.Errorf("failed to save flagged groups: %w", err)
	}

	w.logger.Info("Checked group trackings", zap.Int("flagged_groups", len(flaggedGroups)))
	return nil
}

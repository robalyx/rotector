package maintenance

import (
	"context"
	"time"

	"github.com/disgoorg/disgo"
	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/gateway"
	"github.com/disgoorg/snowflake/v2"
	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/robalyx/rotector/internal/database"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/progress"
	"github.com/robalyx/rotector/internal/roblox/checker"
	"github.com/robalyx/rotector/internal/roblox/fetcher"
	"github.com/robalyx/rotector/internal/setup"
	"github.com/robalyx/rotector/internal/worker/core"
	"go.uber.org/zap"
)

// Worker handles all maintenance operations.
type Worker struct {
	db                      database.Client
	bot                     bot.Client
	roAPI                   *api.API
	bar                     *progress.Bar
	userFetcher             *fetcher.UserFetcher
	groupFetcher            *fetcher.GroupFetcher
	thumbnailFetcher        *fetcher.ThumbnailFetcher
	groupChecker            *checker.GroupChecker
	reporter                *core.StatusReporter
	logger                  *zap.Logger
	reviewerInfoMaxAge      time.Duration
	userBatchSize           int
	groupBatchSize          int
	trackBatchSize          int
	thumbnailUserBatchSize  int
	thumbnailGroupBatchSize int
	minGroupFlaggedUsers    int
	minFlaggedOverride      int
	minFlaggedPercent       float64
}

// New creates a new maintenance worker.
func New(app *setup.App, bar *progress.Bar, logger *zap.Logger) *Worker {
	userFetcher := fetcher.NewUserFetcher(app, logger)
	groupFetcher := fetcher.NewGroupFetcher(app.RoAPI, logger)
	thumbnailFetcher := fetcher.NewThumbnailFetcher(app.RoAPI, logger)
	reporter := core.NewStatusReporter(app.StatusClient, "maintenance", logger)
	groupChecker := checker.NewGroupChecker(app.DB, logger,
		app.Config.Worker.ThresholdLimits.MaxGroupMembersTrack,
		app.Config.Worker.ThresholdLimits.MinFlaggedOverride,
		app.Config.Worker.ThresholdLimits.MinFlaggedPercentage,
	)

	// Create Discord client
	client, err := disgo.New(app.Config.Bot.Discord.Token,
		bot.WithGatewayConfigOpts(
			gateway.WithIntents(
				gateway.IntentGuilds,
				gateway.IntentGuildMembers,
			),
		),
	)
	if err != nil {
		logger.Fatal("failed to create Discord client", zap.Error(err))
	}

	return &Worker{
		db:                      app.DB,
		bot:                     client,
		roAPI:                   app.RoAPI,
		bar:                     bar,
		userFetcher:             userFetcher,
		groupFetcher:            groupFetcher,
		thumbnailFetcher:        thumbnailFetcher,
		groupChecker:            groupChecker,
		reporter:                reporter,
		logger:                  logger.Named("maintenance_worker"),
		reviewerInfoMaxAge:      24 * time.Hour,
		userBatchSize:           app.Config.Worker.BatchSizes.PurgeUsers,
		groupBatchSize:          app.Config.Worker.BatchSizes.PurgeGroups,
		trackBatchSize:          app.Config.Worker.BatchSizes.TrackGroups,
		thumbnailUserBatchSize:  app.Config.Worker.BatchSizes.ThumbnailUsers,
		thumbnailGroupBatchSize: app.Config.Worker.BatchSizes.ThumbnailGroups,
		minGroupFlaggedUsers:    app.Config.Worker.ThresholdLimits.MinGroupFlaggedUsers,
		minFlaggedOverride:      app.Config.Worker.ThresholdLimits.MinFlaggedOverride,
		minFlaggedPercent:       app.Config.Worker.ThresholdLimits.MinFlaggedPercentage,
	}
}

// Start begins the maintenance worker's main loop.
func (w *Worker) Start() {
	w.logger.Info("Maintenance Worker started", zap.String("workerID", w.reporter.GetWorkerID()))
	w.reporter.Start()
	defer w.reporter.Stop()

	w.bar.SetTotal(100)

	for {
		w.bar.Reset()
		w.reporter.SetHealthy(true)

		// Step 1: Process banned users (10%)
		w.processBannedUsers()

		// Step 2: Process locked groups (20%)
		w.processLockedGroups()

		// Step 3: Process cleared users (30%)
		w.processClearedUsers()

		// Step 4: Process cleared groups (40%)
		w.processClearedGroups()

		// Step 5: Process group tracking (50%)
		w.processGroupTracking()

		// Step 6: Process user thumbnails (60%)
		w.processUserThumbnails()

		// Step 7: Process group thumbnails (70%)
		w.processGroupThumbnails()

		// Step 8: Process old Discord server members (80%)
		w.processOldServerMembers()

		// Step 9: Process reviewer info (90%)
		w.processReviewerInfo()

		// Step 10: Completed (100%)
		w.bar.SetStepMessage("Completed", 100)
		w.reporter.UpdateStatus("Completed", 100)

		// Short pause before next iteration
		time.Sleep(10 * time.Second)
	}
}

// processBannedUsers checks for and marks banned users.
func (w *Worker) processBannedUsers() {
	w.bar.SetStepMessage("Processing banned users", 10)
	w.reporter.UpdateStatus("Processing banned users", 10)

	// Get users to check
	users, currentlyBanned, err := w.db.Model().User().GetUsersToCheck(context.Background(), w.userBatchSize)
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
	bannedUserIDs, err := w.userFetcher.FetchBannedUsers(context.Background(), users)
	if err != nil {
		w.logger.Error("Error fetching banned users", zap.Error(err))
		w.reporter.SetHealthy(false)
		return
	}

	// Create map of newly banned users for O(1) lookup
	bannedMap := make(map[uint64]struct{}, len(bannedUserIDs))
	for _, id := range bannedUserIDs {
		bannedMap[id] = struct{}{}
	}

	// Find users that are no longer banned
	var unbannedUserIDs []uint64
	for _, id := range currentlyBanned {
		if _, ok := bannedMap[id]; !ok {
			unbannedUserIDs = append(unbannedUserIDs, id)
		}
	}

	// Mark banned users
	if len(bannedUserIDs) > 0 {
		err = w.db.Model().User().MarkUsersBanStatus(context.Background(), bannedUserIDs, true)
		if err != nil {
			w.logger.Error("Error marking banned users", zap.Error(err))
			w.reporter.SetHealthy(false)
			return
		}
		w.logger.Info("Marked banned users", zap.Int("count", len(bannedUserIDs)))
	}

	// Unmark users that are no longer banned
	if len(unbannedUserIDs) > 0 {
		err = w.db.Model().User().MarkUsersBanStatus(context.Background(), unbannedUserIDs, false)
		if err != nil {
			w.logger.Error("Error unmarking banned users", zap.Error(err))
			w.reporter.SetHealthy(false)
			return
		}
		w.logger.Info("Unmarked banned users", zap.Int("count", len(unbannedUserIDs)))
	}
}

// processLockedGroups checks for and marks locked groups.
func (w *Worker) processLockedGroups() {
	w.bar.SetStepMessage("Processing locked groups", 20)
	w.reporter.UpdateStatus("Processing locked groups", 20)

	// Get groups to check
	groups, currentlyLocked, err := w.db.Model().Group().GetGroupsToCheck(context.Background(), w.groupBatchSize)
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
	lockedGroupIDs, err := w.groupFetcher.FetchLockedGroups(context.Background(), groups)
	if err != nil {
		w.logger.Error("Error fetching locked groups", zap.Error(err))
		w.reporter.SetHealthy(false)
		return
	}

	// Create map of newly locked groups for O(1) lookup
	lockedMap := make(map[uint64]struct{}, len(lockedGroupIDs))
	for _, id := range lockedGroupIDs {
		lockedMap[id] = struct{}{}
	}

	// Find groups that are no longer locked
	var unlockedGroupIDs []uint64
	for _, id := range currentlyLocked {
		if _, ok := lockedMap[id]; !ok {
			unlockedGroupIDs = append(unlockedGroupIDs, id)
		}
	}

	// Mark locked groups
	if len(lockedGroupIDs) > 0 {
		err = w.db.Model().Group().MarkGroupsLockStatus(context.Background(), lockedGroupIDs, true)
		if err != nil {
			w.logger.Error("Error marking locked groups", zap.Error(err))
			w.reporter.SetHealthy(false)
			return
		}
		w.logger.Info("Marked locked groups", zap.Int("count", len(lockedGroupIDs)))
	}

	// Unmark groups that are no longer locked
	if len(unlockedGroupIDs) > 0 {
		err = w.db.Model().Group().MarkGroupsLockStatus(context.Background(), unlockedGroupIDs, false)
		if err != nil {
			w.logger.Error("Error unmarking locked groups", zap.Error(err))
			w.reporter.SetHealthy(false)
			return
		}
		w.logger.Info("Unmarked locked groups", zap.Int("count", len(unlockedGroupIDs)))
	}
}

// processClearedUsers removes old cleared users.
func (w *Worker) processClearedUsers() {
	w.bar.SetStepMessage("Processing cleared users", 30)
	w.reporter.UpdateStatus("Processing cleared users", 30)

	cutOffDate := time.Now().AddDate(0, 0, -30)
	affected, err := w.db.Service().User().PurgeOldClearedUsers(context.Background(), cutOffDate)
	if err != nil {
		w.logger.Error("Error purging old cleared users", zap.Error(err))
		w.reporter.SetHealthy(false)
		return
	}

	if affected > 0 {
		w.logger.Info("Purged old cleared users",
			zap.Int("affected", affected),
			zap.Time("cutOffDate", cutOffDate))
	}
}

// processClearedGroups removes old cleared groups.
func (w *Worker) processClearedGroups() {
	w.bar.SetStepMessage("Processing cleared groups", 40)
	w.reporter.UpdateStatus("Processing cleared groups", 40)

	cutOffDate := time.Now().AddDate(0, 0, -30)
	affected, err := w.db.Model().Group().PurgeOldClearedGroups(context.Background(), cutOffDate)
	if err != nil {
		w.logger.Error("Error purging old cleared groups", zap.Error(err))
		w.reporter.SetHealthy(false)
		return
	}

	if affected > 0 {
		w.logger.Info("Purged old cleared groups",
			zap.Int("affected", affected),
			zap.Time("cutOffDate", cutOffDate))
	}
}

// processGroupTracking manages group tracking data.
func (w *Worker) processGroupTracking() {
	w.bar.SetStepMessage("Processing group tracking", 50)
	w.reporter.UpdateStatus("Processing group tracking", 50)

	// Get groups to check
	groupsWithUsers, err := w.db.Model().Tracking().GetGroupTrackingsToCheck(
		context.Background(),
		w.trackBatchSize,
		w.minGroupFlaggedUsers,
		w.minFlaggedOverride,
	)
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

	// Get existing groups from database
	existingGroups, err := w.db.Model().Group().GetGroupsByIDs(
		context.Background(),
		groupIDs,
		types.GroupFieldID,
	)
	if err != nil {
		w.logger.Error("Failed to check existing groups", zap.Error(err))
		w.reporter.SetHealthy(false)
		return
	}

	// Separate new and existing groups
	newGroupIDs := make([]uint64, 0)
	existingGroupIDs := make([]uint64, 0)
	for _, groupID := range groupIDs {
		if _, exists := existingGroups[groupID]; exists {
			existingGroupIDs = append(existingGroupIDs, groupID)
		} else {
			newGroupIDs = append(newGroupIDs, groupID)
		}
	}

	// Process new groups
	newlyFlaggedIDs := w.processNewGroups(context.Background(), newGroupIDs, groupsWithUsers)
	if len(newlyFlaggedIDs) > 0 {
		existingGroupIDs = append(existingGroupIDs, newlyFlaggedIDs...)
	}

	// Update tracking entries to mark them as flagged for all groups
	if len(existingGroupIDs) > 0 {
		if err := w.db.Model().Tracking().UpdateFlaggedGroups(context.Background(), existingGroupIDs); err != nil {
			w.logger.Error("Failed to update tracking entries", zap.Error(err))
			return
		}
	}

	w.logger.Info("Processed group trackings",
		zap.Int("totalGroups", len(groupIDs)),
		zap.Int("existingGroups", len(existingGroupIDs)-len(newlyFlaggedIDs)),
		zap.Int("newGroups", len(newlyFlaggedIDs)))
}

// processNewGroups handles fetching and processing of new groups.
// Returns the IDs of newly flagged groups.
func (w *Worker) processNewGroups(ctx context.Context, newGroupIDs []uint64, groupsWithUsers map[uint64][]uint64) []uint64 {
	if len(newGroupIDs) == 0 {
		return nil
	}

	// Load group information from API
	groupInfos := w.groupFetcher.FetchGroupInfos(ctx, newGroupIDs)
	if len(groupInfos) == 0 {
		return nil
	}

	// Check which groups exceed the percentage threshold
	flaggedGroups := w.groupChecker.CheckGroupPercentages(ctx, groupInfos, groupsWithUsers)
	if len(flaggedGroups) == 0 {
		return nil
	}

	// Add thumbnails to flagged groups
	flaggedGroups = w.thumbnailFetcher.AddGroupImageURLs(ctx, flaggedGroups)

	// Save flagged groups to database
	if err := w.db.Service().Group().SaveGroups(ctx, flaggedGroups); err != nil {
		w.logger.Error("Failed to save flagged groups", zap.Error(err))
		return nil
	}

	// Extract and return the IDs of newly flagged groups
	newlyFlaggedIDs := make([]uint64, 0, len(flaggedGroups))
	for groupID := range flaggedGroups {
		newlyFlaggedIDs = append(newlyFlaggedIDs, groupID)
	}
	return newlyFlaggedIDs
}

// processUserThumbnails updates user thumbnails.
func (w *Worker) processUserThumbnails() {
	w.bar.SetStepMessage("Processing user thumbnails", 60)
	w.reporter.UpdateStatus("Processing user thumbnails", 60)

	// Get users that need thumbnail updates
	users, err := w.db.Model().User().GetUsersForThumbnailUpdate(context.Background(), w.thumbnailUserBatchSize)
	if err != nil {
		w.logger.Error("Error getting users for thumbnail update", zap.Error(err))
		w.reporter.SetHealthy(false)
		return
	}

	if len(users) == 0 {
		w.logger.Info("No users need thumbnail updates")
		return
	}

	// Update thumbnails
	thumbnailMap := w.thumbnailFetcher.GetImageURLs(context.Background(), users)

	// Convert users to review users
	now := time.Now()
	reviewUsers := make(map[uint64]*types.ReviewUser, len(users))
	for id, user := range users {
		if thumbnail, ok := thumbnailMap[id]; ok {
			user.ThumbnailURL = thumbnail
			user.LastThumbnailUpdate = now
		}
		reviewUsers[id] = &types.ReviewUser{
			User: user,
		}
	}

	// Save updated users
	if err := w.db.Service().User().SaveUsers(context.Background(), reviewUsers); err != nil {
		w.logger.Error("Error saving updated user thumbnails", zap.Error(err))
		w.reporter.SetHealthy(false)
		return
	}

	w.logger.Info("Updated user thumbnails",
		zap.Int("processedCount", len(users)),
		zap.Int("updatedCount", len(thumbnailMap)))
}

// processGroupThumbnails updates group thumbnails.
func (w *Worker) processGroupThumbnails() {
	w.bar.SetStepMessage("Processing group thumbnails", 70)
	w.reporter.UpdateStatus("Processing group thumbnails", 70)

	// Get groups that need thumbnail updates
	groups, err := w.db.Model().Group().GetGroupsForThumbnailUpdate(context.Background(), w.thumbnailGroupBatchSize)
	if err != nil {
		w.logger.Error("Error getting groups for thumbnail update", zap.Error(err))
		w.reporter.SetHealthy(false)
		return
	}

	if len(groups) == 0 {
		w.logger.Info("No groups need thumbnail updates")
		return
	}

	// Update thumbnails
	updatedGroups := w.thumbnailFetcher.AddGroupImageURLs(context.Background(), groups)

	// Save updated groups
	if err := w.db.Service().Group().SaveGroups(context.Background(), groups); err != nil {
		w.logger.Error("Error saving updated group thumbnails", zap.Error(err))
		w.reporter.SetHealthy(false)
		return
	}

	w.logger.Info("Updated group thumbnails",
		zap.Int("processedCount", len(groups)),
		zap.Int("updatedCount", len(updatedGroups)))
}

// processOldServerMembers removes Discord server member records older than 7 days.
func (w *Worker) processOldServerMembers() {
	w.bar.SetStepMessage("Processing old Discord server members", 80)
	w.reporter.UpdateStatus("Processing old Discord server members", 80)

	cutoffDate := time.Now().AddDate(0, 0, -7) // 7 days ago
	affected, err := w.db.Model().Sync().PurgeOldServerMembers(context.Background(), cutoffDate)
	if err != nil {
		w.logger.Error("Error purging old Discord server members", zap.Error(err))
		w.reporter.SetHealthy(false)
		return
	}

	if affected > 0 {
		w.logger.Info("Purged old Discord server members",
			zap.Int("affected", affected),
			zap.Time("cutoffDate", cutoffDate))
	}
}

// processReviewerInfo updates cached Discord user information for reviewers.
func (w *Worker) processReviewerInfo() {
	w.bar.SetStepMessage("Processing reviewer info", 90)
	w.reporter.UpdateStatus("Processing reviewer info", 90)

	// Get bot settings to get reviewer IDs
	settings, err := w.db.Model().Setting().GetBotSettings(context.Background())
	if err != nil {
		w.logger.Error("Failed to get bot settings", zap.Error(err))
		w.reporter.SetHealthy(false)
		return
	}

	// Get existing reviewer info that needs updating
	reviewerInfos, err := w.db.Model().Reviewer().GetReviewerInfosForUpdate(context.Background(), w.reviewerInfoMaxAge)
	if err != nil {
		w.logger.Error("Failed to get reviewer infos", zap.Error(err))
		w.reporter.SetHealthy(false)
		return
	}

	// Build list of reviewers to update
	updatedReviewers := make([]*types.ReviewerInfo, 0, len(settings.ReviewerIDs))
	now := time.Now()

	for _, reviewerID := range settings.ReviewerIDs {
		// Skip if reviewer info is fresh
		if info, exists := reviewerInfos[reviewerID]; exists && info.UpdatedAt.Add(w.reviewerInfoMaxAge).After(now) {
			continue
		}

		// Get user info from Discord
		user, err := w.bot.Rest().GetUser(snowflake.ID(reviewerID))
		if err != nil {
			w.logger.Error("Failed to get Discord user",
				zap.Error(err),
				zap.Uint64("reviewer_id", reviewerID))
			continue
		}

		// Get user's display name
		displayName := user.GlobalName
		if displayName == nil {
			displayName = &user.Username
		}

		// Add to update list
		updatedReviewers = append(updatedReviewers, &types.ReviewerInfo{
			UserID:      reviewerID,
			Username:    user.Username,
			DisplayName: *displayName,
			UpdatedAt:   now,
		})
	}

	// Save updated reviewer info
	if len(updatedReviewers) > 0 {
		err = w.db.Model().Reviewer().SaveReviewerInfos(context.Background(), updatedReviewers)
		if err != nil {
			w.logger.Error("Failed to save reviewer infos", zap.Error(err))
			w.reporter.SetHealthy(false)
			return
		}

		w.logger.Info("Updated reviewer info",
			zap.Int("count", len(updatedReviewers)))
	}
}

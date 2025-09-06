package maintenance

import (
	"context"
	"time"

	"github.com/disgoorg/disgo"
	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/gateway"
	"github.com/disgoorg/snowflake/v2"
	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/robalyx/rotector/internal/cloudflare"
	"github.com/robalyx/rotector/internal/database"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/roblox/checker"
	"github.com/robalyx/rotector/internal/roblox/fetcher"
	"github.com/robalyx/rotector/internal/setup"
	"github.com/robalyx/rotector/internal/tui/components"
	"github.com/robalyx/rotector/internal/worker/core"
	"github.com/robalyx/rotector/pkg/utils"
	"go.uber.org/zap"
)

// Worker handles all maintenance operations.
type Worker struct {
	db                      database.Client
	d1Client                *cloudflare.Client
	bot                     *bot.Client
	roAPI                   *api.API
	bar                     *components.ProgressBar
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
func New(app *setup.App, bar *components.ProgressBar, logger *zap.Logger, instanceID string) *Worker {
	userFetcher := fetcher.NewUserFetcher(app, logger)
	groupFetcher := fetcher.NewGroupFetcher(app.RoAPI, logger)
	thumbnailFetcher := fetcher.NewThumbnailFetcher(app.RoAPI, logger)
	reporter := core.NewStatusReporter(app.StatusClient, "maintenance", instanceID, logger)
	groupChecker := checker.NewGroupChecker(app, logger)

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
		d1Client:                app.D1Client,
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
func (w *Worker) Start(ctx context.Context) {
	w.logger.Info("Maintenance Worker started", zap.String("workerID", w.reporter.GetWorkerID()))

	w.reporter.Start(ctx)
	defer w.reporter.Stop()

	w.bar.SetTotal(100)

	for {
		// Check if context was cancelled
		if utils.ContextGuardWithLog(ctx, w.logger, "Context cancelled, stopping maintenance worker") {
			w.bar.SetStepMessage("Shutting down", 100)
			w.reporter.UpdateStatus("Shutting down", 100)

			return
		}

		w.bar.Reset()
		w.reporter.SetHealthy(true)

		// Step 1: Process banned users (10%)
		w.processBannedUsers(ctx)

		// Step 2: Process locked groups (20%)
		w.processLockedGroups(ctx)

		// Step 3: Process cleared users (30%)
		w.processClearedUsers(ctx)

		// Step 4: Process group tracking (40%)
		w.processGroupTracking(ctx)

		// Step 5: Process user thumbnails (50%)
		w.processUserThumbnails(ctx)

		// Step 6: Process group thumbnails (60%)
		w.processGroupThumbnails(ctx)

		// Step 7: Process old Discord server members (70%)
		w.processOldServerMembers(ctx)

		// Step 8: Process reviewer info (80%)
		w.processReviewerInfo(ctx)

		// Step 9: Completed (100%)
		w.bar.SetStepMessage("Completed", 100)
		w.reporter.UpdateStatus("Completed", 100)

		// Short pause before next iteration
		if !utils.IntervalSleep(ctx, 10*time.Second, w.logger, "maintenance worker") {
			return
		}
	}
}

// processBannedUsers checks for and marks banned users.
func (w *Worker) processBannedUsers(ctx context.Context) {
	w.bar.SetStepMessage("Processing banned users", 10)
	w.reporter.UpdateStatus("Processing banned users", 10)

	// Get users to check
	users, currentlyBanned, err := w.db.Model().User().GetUsersToCheck(ctx, w.userBatchSize)
	if err != nil {
		w.logger.Error("Error getting users to check", zap.Error(err))
		w.reporter.SetHealthy(false)

		return
	}

	if len(users) == 0 {
		w.logger.Debug("No users to check for bans")
		return
	}

	// Check for banned users
	bannedUserIDs, err := w.userFetcher.FetchBannedUsers(ctx, users)
	if err != nil {
		w.logger.Error("Error fetching banned users", zap.Error(err))
		w.reporter.SetHealthy(false)

		return
	}

	// Create map of newly banned users for O(1) lookup
	bannedMap := make(map[int64]struct{}, len(bannedUserIDs))
	for _, id := range bannedUserIDs {
		bannedMap[id] = struct{}{}
	}

	// Find users that are no longer banned
	var unbannedUserIDs []int64

	for _, id := range currentlyBanned {
		if _, ok := bannedMap[id]; !ok {
			unbannedUserIDs = append(unbannedUserIDs, id)
		}
	}

	// Mark banned users
	if len(bannedUserIDs) > 0 {
		err = w.db.Model().User().MarkUsersBanStatus(ctx, bannedUserIDs, true)
		if err != nil {
			w.logger.Error("Error marking banned users", zap.Error(err))
			w.reporter.SetHealthy(false)

			return
		}

		// Update D1 database ban status
		if err := w.d1Client.UserFlags.UpdateBanStatus(ctx, bannedUserIDs, true); err != nil {
			w.logger.Error("Error updating banned users in D1", zap.Error(err))
			w.reporter.SetHealthy(false)

			return
		}

		// Auto-confirm banned users since Roblox ban is definitive evidence
		bannedUsers := make([]*types.ReviewUser, 0, len(bannedUserIDs))
		for _, userID := range bannedUserIDs {
			bannedUsers = append(bannedUsers, &types.ReviewUser{
				User: &types.User{
					ID: userID,
				},
			})
		}

		// Confirm banned users with system reviewer ID
		if err := w.db.Service().User().ConfirmUsers(ctx, bannedUsers, 0); err != nil {
			w.logger.Error("Error confirming banned users", zap.Error(err))
			w.reporter.SetHealthy(false)

			return
		}

		// Add confirmed users to D1 database
		for _, user := range bannedUsers {
			if err := w.d1Client.UserFlags.AddConfirmed(ctx, user, 0); err != nil {
				w.logger.Error("Error adding confirmed banned user to D1",
					zap.Error(err),
					zap.Int64("userID", user.ID))
			}
		}

		w.logger.Info("Marked and confirmed banned users", zap.Int("count", len(bannedUserIDs)))
	}

	// Unmark users that are no longer banned
	if len(unbannedUserIDs) > 0 {
		err = w.db.Model().User().MarkUsersBanStatus(ctx, unbannedUserIDs, false)
		if err != nil {
			w.logger.Error("Error unmarking banned users", zap.Error(err))
			w.reporter.SetHealthy(false)

			return
		}

		// Update D1 database ban status
		if err := w.d1Client.UserFlags.UpdateBanStatus(ctx, unbannedUserIDs, false); err != nil {
			w.logger.Error("Error updating unbanned users in D1", zap.Error(err))
			w.reporter.SetHealthy(false)

			return
		}

		w.logger.Info("Unmarked banned users", zap.Int("count", len(unbannedUserIDs)))
	}
}

// processLockedGroups checks for and marks locked groups.
func (w *Worker) processLockedGroups(ctx context.Context) {
	w.bar.SetStepMessage("Processing locked groups", 20)
	w.reporter.UpdateStatus("Processing locked groups", 20)

	// Get groups to check
	groups, currentlyLocked, err := w.db.Model().Group().GetGroupsToCheck(ctx, w.groupBatchSize)
	if err != nil {
		w.logger.Error("Error getting groups to check", zap.Error(err))
		w.reporter.SetHealthy(false)

		return
	}

	if len(groups) == 0 {
		w.logger.Debug("No groups to check for locks")
		return
	}

	// Check for locked groups
	lockedGroupIDs, err := w.groupFetcher.FetchLockedGroups(ctx, groups)
	if err != nil {
		w.logger.Error("Error fetching locked groups", zap.Error(err))
		w.reporter.SetHealthy(false)

		return
	}

	// Create map of newly locked groups for O(1) lookup
	lockedMap := make(map[int64]struct{}, len(lockedGroupIDs))
	for _, id := range lockedGroupIDs {
		lockedMap[id] = struct{}{}
	}

	// Find groups that are no longer locked
	var unlockedGroupIDs []int64

	for _, id := range currentlyLocked {
		if _, ok := lockedMap[id]; !ok {
			unlockedGroupIDs = append(unlockedGroupIDs, id)
		}
	}

	// Mark locked groups
	if len(lockedGroupIDs) > 0 {
		err = w.db.Model().Group().MarkGroupsLockStatus(ctx, lockedGroupIDs, true)
		if err != nil {
			w.logger.Error("Error marking locked groups", zap.Error(err))
			w.reporter.SetHealthy(false)

			return
		}

		w.logger.Info("Marked locked groups", zap.Int("count", len(lockedGroupIDs)))
	}

	// Unmark groups that are no longer locked
	if len(unlockedGroupIDs) > 0 {
		err = w.db.Model().Group().MarkGroupsLockStatus(ctx, unlockedGroupIDs, false)
		if err != nil {
			w.logger.Error("Error unmarking locked groups", zap.Error(err))
			w.reporter.SetHealthy(false)

			return
		}

		w.logger.Info("Unmarked locked groups", zap.Int("count", len(unlockedGroupIDs)))
	}
}

// processClearedUsers removes old cleared users.
func (w *Worker) processClearedUsers(ctx context.Context) {
	w.bar.SetStepMessage("Processing cleared users", 30)
	w.reporter.UpdateStatus("Processing cleared users", 30)

	cutOffDate := time.Now().AddDate(-1, 0, 0)

	affected, err := w.db.Service().User().PurgeOldClearedUsers(ctx, cutOffDate)
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

// processGroupTracking manages group tracking data.
func (w *Worker) processGroupTracking(ctx context.Context) {
	w.bar.SetStepMessage("Processing group tracking", 40)
	w.reporter.UpdateStatus("Processing group tracking", 40)

	// Get groups to check
	groupsWithUsers, err := w.db.Model().Tracking().GetGroupTrackingsToCheck(
		ctx,
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
		w.logger.Debug("No groups to check for tracking")
		return
	}

	// Extract group IDs for batch lookup
	groupIDs := make([]int64, 0, len(groupsWithUsers))
	for groupID := range groupsWithUsers {
		groupIDs = append(groupIDs, groupID)
	}

	// Get existing groups from database
	existingGroups, err := w.db.Model().Group().GetGroupsByIDs(
		ctx,
		groupIDs,
		types.GroupFieldID,
	)
	if err != nil {
		w.logger.Error("Failed to check existing groups", zap.Error(err))
		w.reporter.SetHealthy(false)

		return
	}

	// Separate new and existing groups
	newGroupIDs := make([]int64, 0)
	existingGroupIDs := make([]int64, 0)

	for _, groupID := range groupIDs {
		if _, exists := existingGroups[groupID]; exists {
			existingGroupIDs = append(existingGroupIDs, groupID)
		} else {
			newGroupIDs = append(newGroupIDs, groupID)
		}
	}

	// Process new groups
	newlyFlaggedIDs := w.processNewGroups(ctx, newGroupIDs, groupsWithUsers)
	if len(newlyFlaggedIDs) > 0 {
		existingGroupIDs = append(existingGroupIDs, newlyFlaggedIDs...)
	}

	// Update tracking entries to mark them as flagged for all groups
	if len(existingGroupIDs) > 0 {
		if err := w.db.Model().Tracking().UpdateFlaggedGroups(ctx, existingGroupIDs); err != nil {
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
func (w *Worker) processNewGroups(ctx context.Context, newGroupIDs []int64, groupsWithUsers map[int64][]int64) []int64 {
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

	// Add flagged groups to D1 database
	if err := w.d1Client.GroupFlags.AddFlagged(ctx, flaggedGroups); err != nil {
		w.logger.Error("Failed to add flagged groups to D1 database", zap.Error(err))
	}

	// Extract and return the IDs of newly flagged groups
	newlyFlaggedIDs := make([]int64, 0, len(flaggedGroups))
	for groupID := range flaggedGroups {
		newlyFlaggedIDs = append(newlyFlaggedIDs, groupID)
	}

	return newlyFlaggedIDs
}

// processUserThumbnails updates user thumbnails.
func (w *Worker) processUserThumbnails(ctx context.Context) {
	w.bar.SetStepMessage("Processing user thumbnails", 50)
	w.reporter.UpdateStatus("Processing user thumbnails", 50)

	// Get users that need thumbnail updates
	users, err := w.db.Model().User().GetUsersForThumbnailUpdate(ctx, w.thumbnailUserBatchSize)
	if err != nil {
		w.logger.Error("Error getting users for thumbnail update", zap.Error(err))
		w.reporter.SetHealthy(false)

		return
	}

	if len(users) == 0 {
		w.logger.Debug("No users need thumbnail updates")
		return
	}

	// Update thumbnails
	thumbnailMap := w.thumbnailFetcher.GetImageURLs(ctx, users)

	// Convert users to review users
	now := time.Now()

	reviewUsers := make(map[int64]*types.ReviewUser, len(users))
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
	if err := w.db.Service().User().SaveUsers(ctx, reviewUsers); err != nil {
		w.logger.Error("Error saving updated user thumbnails", zap.Error(err))
		w.reporter.SetHealthy(false)

		return
	}

	w.logger.Info("Updated user thumbnails",
		zap.Int("processedCount", len(users)),
		zap.Int("updatedCount", len(thumbnailMap)))
}

// processGroupThumbnails updates group thumbnails.
func (w *Worker) processGroupThumbnails(ctx context.Context) {
	w.bar.SetStepMessage("Processing group thumbnails", 60)
	w.reporter.UpdateStatus("Processing group thumbnails", 60)

	// Get groups that need thumbnail updates
	groups, err := w.db.Model().Group().GetGroupsForThumbnailUpdate(ctx, w.thumbnailGroupBatchSize)
	if err != nil {
		w.logger.Error("Error getting groups for thumbnail update", zap.Error(err))
		w.reporter.SetHealthy(false)

		return
	}

	if len(groups) == 0 {
		w.logger.Debug("No groups need thumbnail updates")
		return
	}

	// Update thumbnails
	updatedGroups := w.thumbnailFetcher.AddGroupImageURLs(ctx, groups)

	// Save updated groups
	if err := w.db.Service().Group().SaveGroups(ctx, groups); err != nil {
		w.logger.Error("Error saving updated group thumbnails", zap.Error(err))
		w.reporter.SetHealthy(false)

		return
	}

	w.logger.Info("Updated group thumbnails",
		zap.Int("processedCount", len(groups)),
		zap.Int("updatedCount", len(updatedGroups)))
}

// processOldServerMembers removes Discord server member records older than 7 days.
func (w *Worker) processOldServerMembers(ctx context.Context) {
	w.bar.SetStepMessage("Processing old Discord server members", 70)
	w.reporter.UpdateStatus("Processing old Discord server members", 70)

	cutoffDate := time.Now().AddDate(0, 0, -7) // 7 days ago

	affected, err := w.db.Model().Sync().PurgeOldServerMembers(ctx, cutoffDate)
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
func (w *Worker) processReviewerInfo(ctx context.Context) {
	w.bar.SetStepMessage("Processing reviewer info", 80)
	w.reporter.UpdateStatus("Processing reviewer info", 80)

	// Get bot settings to get reviewer IDs
	settings, err := w.db.Model().Setting().GetBotSettings(ctx)
	if err != nil {
		w.logger.Error("Failed to get bot settings", zap.Error(err))
		w.reporter.SetHealthy(false)

		return
	}

	// Get existing reviewer info that needs updating
	reviewerInfos, err := w.db.Model().Reviewer().GetReviewerInfosForUpdate(ctx, w.reviewerInfoMaxAge)
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
		user, err := w.bot.Rest.GetUser(snowflake.ID(reviewerID))
		if err != nil {
			w.logger.Error("Failed to get Discord user",
				zap.Error(err),
				zap.Uint64("reviewerID", reviewerID))

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
		err = w.db.Model().Reviewer().SaveReviewerInfos(ctx, updatedReviewers)
		if err != nil {
			w.logger.Error("Failed to save reviewer infos", zap.Error(err))
			w.reporter.SetHealthy(false)

			return
		}

		w.logger.Info("Updated reviewer info",
			zap.Int("count", len(updatedReviewers)))
	}
}

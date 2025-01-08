package ai

import (
	"context"
	"fmt"
	"time"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/jaxron/roapi.go/pkg/api/resources/groups"
	"github.com/rotector/rotector/internal/common/client/checker"
	"github.com/rotector/rotector/internal/common/client/fetcher"
	"github.com/rotector/rotector/internal/common/progress"
	"github.com/rotector/rotector/internal/common/setup"
	"github.com/rotector/rotector/internal/common/storage/database"
	"github.com/rotector/rotector/internal/worker/core"
	"go.uber.org/zap"
)

// GroupWorker processes group member lists by checking each member's
// status and analyzing their profiles for inappropriate content.
type GroupWorker struct {
	db               *database.Client
	roAPI            *api.API
	bar              *progress.Bar
	userFetcher      *fetcher.UserFetcher
	userChecker      *checker.UserChecker
	reporter         *core.StatusReporter
	logger           *zap.Logger
	batchSize        int
	flaggedThreshold int
}

// NewGroupWorker creates a GroupWorker.
func NewGroupWorker(app *setup.App, bar *progress.Bar, logger *zap.Logger) *GroupWorker {
	userFetcher := fetcher.NewUserFetcher(app, logger)
	userChecker := checker.NewUserChecker(app, userFetcher, logger)
	reporter := core.NewStatusReporter(app.StatusClient, "ai", "member", logger)

	return &GroupWorker{
		db:               app.DB,
		roAPI:            app.RoAPI,
		bar:              bar,
		userFetcher:      userFetcher,
		userChecker:      userChecker,
		reporter:         reporter,
		logger:           logger,
		batchSize:        app.Config.Worker.BatchSizes.GroupUsers,
		flaggedThreshold: app.Config.Worker.ThresholdLimits.FlaggedUsers,
	}
}

// Start begins the group worker's main loop:
// 1. Gets a confirmed group to process
// 2. Fetches member lists in batches
// 3. Checks members for inappropriate content
// 4. Repeats until stopped.
func (g *GroupWorker) Start() {
	g.logger.Info("Group Worker started", zap.String("workerID", g.reporter.GetWorkerID()))
	g.reporter.Start()
	defer g.reporter.Stop()

	g.bar.SetTotal(100)

	var oldUserIDs []uint64
	for {
		g.bar.Reset()
		g.reporter.SetHealthy(true)

		// Check flagged users count
		flaggedCount, err := g.db.Users().GetFlaggedUsersCount(context.Background())
		if err != nil {
			g.logger.Error("Error getting flagged users count", zap.Error(err))
			g.reporter.SetHealthy(false)
			time.Sleep(5 * time.Minute)
			continue
		}

		// If above threshold, pause processing
		if flaggedCount >= g.flaggedThreshold {
			g.bar.SetStepMessage(fmt.Sprintf("Paused - %d flagged users exceeds threshold of %d", flaggedCount, g.flaggedThreshold), 0)
			g.reporter.UpdateStatus(fmt.Sprintf("Paused - %d flagged users exceeds threshold", flaggedCount), 0)
			g.logger.Info("Pausing worker - flagged users threshold exceeded",
				zap.Int("flaggedCount", flaggedCount),
				zap.Int("threshold", g.flaggedThreshold))
			time.Sleep(5 * time.Minute)
			continue
		}

		// Step 1: Get next group to process (10%)
		g.bar.SetStepMessage("Fetching next group to process", 10)
		g.reporter.UpdateStatus("Fetching next group to process", 10)
		group, err := g.db.Groups().GetGroupToScan(context.Background())
		if err != nil {
			g.logger.Error("Error getting group to scan", zap.Error(err))
			g.reporter.SetHealthy(false)
			time.Sleep(5 * time.Minute)
			continue
		}

		// Step 2: Get group users (40%)
		g.bar.SetStepMessage("Processing group users", 40)
		g.reporter.UpdateStatus("Processing group users", 40)
		userIDs, err := g.processGroup(group.ID, oldUserIDs)
		if err != nil {
			g.reporter.SetHealthy(false)
			time.Sleep(5 * time.Minute)
			continue
		}

		// Step 3: Fetch user info (70%)
		g.bar.SetStepMessage("Fetching user info", 70)
		g.reporter.UpdateStatus("Fetching user info", 70)
		userInfos := g.userFetcher.FetchInfos(userIDs[:g.batchSize])

		// Step 4: Process users (90%)
		g.bar.SetStepMessage("Processing users", 90)
		g.reporter.UpdateStatus("Processing users", 90)
		failedValidationIDs := g.userChecker.ProcessUsers(userInfos)

		// Step 5: Prepare for next batch
		oldUserIDs = userIDs[g.batchSize:]

		// Add failed validation IDs back to the queue for retry
		if len(failedValidationIDs) > 0 {
			oldUserIDs = append(oldUserIDs, failedValidationIDs...)
			g.logger.Info("Added failed validation IDs for retry",
				zap.Int("failedCount", len(failedValidationIDs)))
		}

		// Step 6: Completed (100%)
		g.bar.SetStepMessage("Completed", 100)
		g.reporter.UpdateStatus("Completed", 100)

		// Short pause before next iteration
		time.Sleep(1 * time.Second)
	}
}

// processGroup builds a list of member IDs to check by:
// 1. Fetching member lists in batches using cursor pagination
// 2. Filtering out already processed users
// 3. Collecting enough IDs to fill a batch.
func (g *GroupWorker) processGroup(groupID uint64, userIDs []uint64) ([]uint64, error) {
	g.logger.Info("Processing group", zap.Uint64("groupID", groupID))

	cursor := ""
	for len(userIDs) < g.batchSize {
		// Fetch group users with cursor pagination
		builder := groups.NewGroupUsersBuilder(groupID).WithLimit(100).WithCursor(cursor)
		groupUsers, err := g.roAPI.Groups().GetGroupUsers(context.Background(), builder.Build())
		if err != nil {
			g.logger.Error("Error fetching group members", zap.Error(err))
			return nil, err
		}

		// If the group has no users, skip it
		if len(groupUsers.Data) == 0 {
			break
		}

		// Extract user IDs from member list
		newUserIDs := make([]uint64, len(groupUsers.Data))
		for i, groupUser := range groupUsers.Data {
			newUserIDs[i] = groupUser.User.UserID
		}

		// Check which users have been recently processed
		existingUsers, err := g.db.Users().GetRecentlyProcessedUsers(context.Background(), newUserIDs)
		if err != nil {
			g.logger.Error("Error checking recently processed users", zap.Error(err))
			continue
		}

		// Add only new users to the userIDs slice
		for _, userID := range newUserIDs {
			if _, exists := existingUsers[userID]; !exists {
				userIDs = append(userIDs, userID)
			}
		}

		g.logger.Info("Fetched group users",
			zap.Uint64("groupID", groupID),
			zap.String("cursor", cursor),
			zap.Int("totalUsers", len(groupUsers.Data)),
			zap.Int("newUsers", len(newUserIDs)-len(existingUsers)),
			zap.Int("userIDs", len(userIDs)))

		// Move to next page if available
		if groupUsers.NextPageCursor == nil {
			break
		}
		cursor = *groupUsers.NextPageCursor
	}

	return userIDs, nil
}

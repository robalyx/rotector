package ai

import (
	"context"
	"fmt"
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

// FriendWorker processes user friend networks by checking each friend's
// status and analyzing their profiles for inappropriate content.
type FriendWorker struct {
	db               *database.Client
	roAPI            *api.API
	bar              *progress.Bar
	userFetcher      *fetcher.UserFetcher
	userChecker      *checker.UserChecker
	friendFetcher    *fetcher.FriendFetcher
	reporter         *core.StatusReporter
	logger           *zap.Logger
	batchSize        int
	flaggedThreshold int
}

// NewFriendWorker creates a FriendWorker.
func NewFriendWorker(app *setup.App, bar *progress.Bar, logger *zap.Logger) *FriendWorker {
	userFetcher := fetcher.NewUserFetcher(app, logger)
	userChecker := checker.NewUserChecker(app, userFetcher, logger)
	friendFetcher := fetcher.NewFriendFetcher(app.RoAPI, logger)
	reporter := core.NewStatusReporter(app.StatusClient, "ai", "friend", logger)

	return &FriendWorker{
		db:               app.DB,
		roAPI:            app.RoAPI,
		bar:              bar,
		userFetcher:      userFetcher,
		userChecker:      userChecker,
		friendFetcher:    friendFetcher,
		reporter:         reporter,
		logger:           logger,
		batchSize:        app.Config.Worker.BatchSizes.FriendUsers,
		flaggedThreshold: app.Config.Worker.ThresholdLimits.FlaggedUsers,
	}
}

// Start begins the friend worker's main loop:
// 1. Gets a batch of users to process
// 2. Fetches friend lists for each user
// 3. Checks friends for inappropriate content
// 4. Repeats until stopped.
func (f *FriendWorker) Start() {
	f.logger.Info("Friend Worker started", zap.String("workerID", f.reporter.GetWorkerID()))
	f.reporter.Start()
	defer f.reporter.Stop()

	f.bar.SetTotal(100)

	var oldFriendIDs []uint64
	for {
		f.bar.Reset()
		f.reporter.SetHealthy(true)

		// Check flagged users count
		flaggedCount, err := f.db.Users().GetFlaggedUsersCount(context.Background())
		if err != nil {
			f.logger.Error("Error getting flagged users count", zap.Error(err))
			f.reporter.SetHealthy(false)
			time.Sleep(5 * time.Minute)
			continue
		}

		// If above threshold, pause processing
		if flaggedCount >= f.flaggedThreshold {
			f.bar.SetStepMessage(fmt.Sprintf("Paused - %d flagged users exceeds threshold of %d", flaggedCount, f.flaggedThreshold), 0)
			f.reporter.UpdateStatus(fmt.Sprintf("Paused - %d flagged users exceeds threshold", flaggedCount), 0)
			f.logger.Info("Pausing worker - flagged users threshold exceeded",
				zap.Int("flaggedCount", flaggedCount),
				zap.Int("threshold", f.flaggedThreshold))
			time.Sleep(5 * time.Minute)
			continue
		}

		// Step 1: Process friends batch (20%)
		f.bar.SetStepMessage("Processing friends batch", 20)
		f.reporter.UpdateStatus("Processing friends batch", 20)
		friendIDs, err := f.processFriendsBatch(oldFriendIDs)
		if err != nil {
			f.reporter.SetHealthy(false)
			time.Sleep(5 * time.Minute)
			continue
		}

		// Step 2: Fetch user info (40%)
		f.bar.SetStepMessage("Fetching user info", 40)
		f.reporter.UpdateStatus("Fetching user info", 40)
		userInfos := f.userFetcher.FetchInfos(friendIDs[:f.batchSize])

		// Step 3: Process users (60%)
		f.bar.SetStepMessage("Processing users", 60)
		f.reporter.UpdateStatus("Processing users", 60)
		failedValidationIDs := f.userChecker.ProcessUsers(userInfos)

		// Step 4: Prepare for next batch
		oldFriendIDs = friendIDs[f.batchSize:]

		// Add failed validation IDs back to the queue for retry
		if len(failedValidationIDs) > 0 {
			oldFriendIDs = append(oldFriendIDs, failedValidationIDs...)
			f.logger.Info("Added failed validation IDs for retry",
				zap.Int("failedCount", len(failedValidationIDs)))
		}

		// Step 5: Completed (100%)
		f.bar.SetStepMessage("Completed", 100)
		f.reporter.UpdateStatus("Completed", 100)

		// Short pause before next iteration
		time.Sleep(1 * time.Second)
	}
}

// processFriendsBatch builds a list of friend IDs to check by:
// 1. Getting confirmed users from the database
// 2. Fetching their friend lists
// 3. Filtering out already processed users
// 4. Collecting enough IDs to fill a batch.
func (f *FriendWorker) processFriendsBatch(friendIDs []uint64) ([]uint64, error) {
	for len(friendIDs) < f.batchSize {
		// Get the next confirmed user
		user, err := f.db.Users().GetUserToScan(context.Background())
		if err != nil {
			f.logger.Error("Error getting user to scan", zap.Error(err))
			return nil, err
		}

		// Fetch friends for the user
		userFriendIDs, err := f.friendFetcher.GetFriends(context.Background(), user.ID)
		if err != nil {
			f.logger.Error("Error fetching friends", zap.Error(err), zap.Uint64("userID", user.ID))
			continue
		}

		// If the user has no friends, skip them
		if len(userFriendIDs) == 0 {
			continue
		}

		// Check which users have been recently processed
		existingUsers, err := f.db.Users().GetRecentlyProcessedUsers(context.Background(), userFriendIDs)
		if err != nil {
			f.logger.Error("Error checking recently processed users", zap.Error(err))
			continue
		}

		// Add only new users to the friendIDs slice
		for _, friendID := range userFriendIDs {
			if _, exists := existingUsers[friendID]; !exists {
				friendIDs = append(friendIDs, friendID)
			}
		}

		f.logger.Info("Fetched friends",
			zap.Int("userFriends", len(userFriendIDs)),
			zap.Int("totalFriends", len(friendIDs)),
			zap.Uint64("userID", user.ID))
	}

	return friendIDs, nil
}

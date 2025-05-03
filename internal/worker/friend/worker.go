package friend

import (
	"context"
	"fmt"
	"time"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/robalyx/rotector/internal/database"
	"github.com/robalyx/rotector/internal/progress"
	"github.com/robalyx/rotector/internal/roblox/checker"
	"github.com/robalyx/rotector/internal/roblox/fetcher"
	"github.com/robalyx/rotector/internal/setup"
	"github.com/robalyx/rotector/internal/worker/core"
	"go.uber.org/zap"
)

// Worker processes user friend networks by checking each friend's
// status and analyzing their profiles for inappropriate content.
type Worker struct {
	db               database.Client
	roAPI            *api.API
	bar              *progress.Bar
	userFetcher      *fetcher.UserFetcher
	userChecker      *checker.UserChecker
	friendFetcher    *fetcher.FriendFetcher
	reporter         *core.StatusReporter
	logger           *zap.Logger
	batchSize        int
	flaggedThreshold int
	pendingFriendIDs []uint64
}

// New creates a new friend worker.
func New(app *setup.App, bar *progress.Bar, logger *zap.Logger) *Worker {
	userFetcher := fetcher.NewUserFetcher(app, logger)
	userChecker := checker.NewUserChecker(app, userFetcher, logger)
	friendFetcher := fetcher.NewFriendFetcher(app, logger)
	reporter := core.NewStatusReporter(app.StatusClient, "friend", logger)

	return &Worker{
		db:               app.DB,
		roAPI:            app.RoAPI,
		bar:              bar,
		userFetcher:      userFetcher,
		userChecker:      userChecker,
		friendFetcher:    friendFetcher,
		reporter:         reporter,
		logger:           logger.Named("friend_worker"),
		batchSize:        app.Config.Worker.BatchSizes.FriendUsers,
		flaggedThreshold: app.Config.Worker.ThresholdLimits.FlaggedUsers,
		pendingFriendIDs: make([]uint64, 0),
	}
}

// Start begins the friend worker's main loop.
func (w *Worker) Start() {
	w.logger.Info("Friend Worker started", zap.String("workerID", w.reporter.GetWorkerID()))
	w.reporter.Start()
	defer w.reporter.Stop()

	w.bar.SetTotal(100)

	for {
		w.bar.Reset()
		w.reporter.SetHealthy(true)

		// Check flagged users count
		flaggedCount, err := w.db.Model().User().GetFlaggedUsersCount(context.Background())
		if err != nil {
			w.logger.Error("Error getting flagged users count", zap.Error(err))
			w.reporter.SetHealthy(false)
			time.Sleep(5 * time.Minute)
			continue
		}

		// If above threshold, pause processing
		if flaggedCount >= w.flaggedThreshold {
			w.bar.SetStepMessage(fmt.Sprintf(
				"Paused - %d flagged users exceeds threshold of %d",
				flaggedCount, w.flaggedThreshold,
			), 0)
			w.reporter.UpdateStatus(fmt.Sprintf(
				"Paused - %d flagged users exceeds threshold",
				flaggedCount,
			), 0)
			w.logger.Info("Pausing worker - flagged users threshold exceeded",
				zap.Int("flaggedCount", flaggedCount),
				zap.Int("threshold", w.flaggedThreshold))
			time.Sleep(5 * time.Minute)
			continue
		}

		// Step 1: Process friends batch (20%)
		w.bar.SetStepMessage("Processing friends batch", 20)
		w.reporter.UpdateStatus("Processing friends batch", 20)
		friendIDs, err := w.processFriendsBatch()
		if err != nil {
			w.reporter.SetHealthy(false)
			time.Sleep(5 * time.Minute)
			continue
		}

		// Step 2: Fetch user info (40%)
		w.bar.SetStepMessage("Fetching user info", 40)
		w.reporter.UpdateStatus("Fetching user info", 40)
		userInfos := w.userFetcher.FetchInfos(context.Background(), friendIDs[:w.batchSize])

		// Step 3: Process users (60%)
		w.bar.SetStepMessage("Processing users", 60)
		w.reporter.UpdateStatus("Processing users", 60)
		w.userChecker.ProcessUsers(userInfos)

		// Step 4: Prepare for next batch
		w.pendingFriendIDs = friendIDs[w.batchSize:]

		// Step 5: Completed (100%)
		w.bar.SetStepMessage("Completed", 100)
		w.reporter.UpdateStatus("Completed", 100)

		// Short pause before next iteration
		time.Sleep(1 * time.Second)
	}
}

// processFriendsBatch builds a list of friend IDs to check.
func (w *Worker) processFriendsBatch() ([]uint64, error) {
	friendIDs := w.pendingFriendIDs

	for len(friendIDs) < w.batchSize {
		// Get the next confirmed user
		user, err := w.db.Model().User().GetUserToScan(context.Background())
		if err != nil {
			w.logger.Error("Error getting user to scan", zap.Error(err))
			w.pendingFriendIDs = friendIDs
			return nil, err
		}

		// Fetch friends for the user
		userFriendIDs, err := w.friendFetcher.GetFriendIDs(context.Background(), user.ID)
		if err != nil {
			w.logger.Error("Error fetching friends", zap.Error(err), zap.Uint64("userID", user.ID))
			continue
		}

		// If the user has no friends, skip them
		if len(userFriendIDs) == 0 {
			continue
		}

		// Check which users have been recently processed
		existingUsers, err := w.db.Model().User().GetRecentlyProcessedUsers(context.Background(), userFriendIDs)
		if err != nil {
			w.logger.Error("Error checking recently processed users", zap.Error(err))
			continue
		}

		// Add only new users to the friendIDs slice
		for _, friendID := range userFriendIDs {
			if _, exists := existingUsers[friendID]; !exists {
				friendIDs = append(friendIDs, friendID)
			}
		}

		w.logger.Info("Fetched friends",
			zap.Int("userFriends", len(userFriendIDs)),
			zap.Int("totalFriends", len(friendIDs)),
			zap.Uint64("userID", user.ID))
	}

	return friendIDs, nil
}

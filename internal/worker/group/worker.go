package group

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/jaxron/roapi.go/pkg/api/resources/groups"
	"github.com/robalyx/rotector/internal/database"
	"github.com/robalyx/rotector/internal/progress"
	"github.com/robalyx/rotector/internal/roblox/checker"
	"github.com/robalyx/rotector/internal/roblox/fetcher"
	"github.com/robalyx/rotector/internal/setup"
	"github.com/robalyx/rotector/internal/worker/core"
	"go.uber.org/zap"
)

// Worker processes group member lists by checking each member's
// status and analyzing their profiles for inappropriate content.
type Worker struct {
	db               database.Client
	roAPI            *api.API
	bar              *progress.Bar
	userFetcher      *fetcher.UserFetcher
	userChecker      *checker.UserChecker
	reporter         *core.StatusReporter
	logger           *zap.Logger
	batchSize        int
	flaggedThreshold int
	currentGroupID   uint64
	currentCursor    string
	pendingUserIDs   []uint64
}

// New creates a new group worker.
func New(app *setup.App, bar *progress.Bar, logger *zap.Logger) *Worker {
	userFetcher := fetcher.NewUserFetcher(app, logger)
	userChecker := checker.NewUserChecker(app, userFetcher, logger)
	reporter := core.NewStatusReporter(app.StatusClient, "group", logger)

	return &Worker{
		db:               app.DB,
		roAPI:            app.RoAPI,
		bar:              bar,
		userFetcher:      userFetcher,
		userChecker:      userChecker,
		reporter:         reporter,
		logger:           logger.Named("group_worker"),
		batchSize:        app.Config.Worker.BatchSizes.GroupUsers,
		flaggedThreshold: app.Config.Worker.ThresholdLimits.FlaggedUsers,
		pendingUserIDs:   make([]uint64, 0),
	}
}

// Start begins the group worker's main loop.
func (w *Worker) Start() {
	w.logger.Info("Group Worker started", zap.String("workerID", w.reporter.GetWorkerID()))
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

		// Step 1: Get next group to process (10%)
		w.bar.SetStepMessage("Fetching next group to process", 10)
		w.reporter.UpdateStatus("Fetching next group to process", 10)

		if w.currentGroupID == 0 {
			group, err := w.db.Model().Group().GetGroupToScan(context.Background())
			if err != nil {
				if !errors.Is(err, sql.ErrNoRows) {
					w.logger.Error("Error getting group to scan", zap.Error(err))
					w.reporter.SetHealthy(false)
				} else {
					w.logger.Warn("No more groups to scan", zap.Error(err))
				}
				time.Sleep(5 * time.Minute)
				continue
			}
			w.currentGroupID = group.ID
		}

		// Step 2: Get group users (40%)
		w.bar.SetStepMessage("Processing group users", 40)
		w.reporter.UpdateStatus("Processing group users", 40)
		userIDs, err := w.processGroup()
		if err != nil {
			w.reporter.SetHealthy(false)
			time.Sleep(5 * time.Minute)
			continue
		}

		// Step 3: Fetch user info (70%)
		w.bar.SetStepMessage("Fetching user info", 70)
		w.reporter.UpdateStatus("Fetching user info", 70)
		userInfos := w.userFetcher.FetchInfos(context.Background(), userIDs[:w.batchSize])

		// Step 4: Process users (90%)
		w.bar.SetStepMessage("Processing users", 90)
		w.reporter.UpdateStatus("Processing users", 90)
		w.userChecker.ProcessUsers(userInfos)

		// Step 5: Prepare for next batch
		w.pendingUserIDs = userIDs[w.batchSize:]

		// Step 6: Completed (100%)
		w.bar.SetStepMessage("Completed", 100)
		w.reporter.UpdateStatus("Completed", 100)

		// Short pause before next iteration
		time.Sleep(1 * time.Second)
	}
}

// processGroup builds a list of member IDs to check.
func (w *Worker) processGroup() ([]uint64, error) {
	userIDs := w.pendingUserIDs

	for {
		// Fetch group users with cursor pagination
		builder := groups.NewGroupUsersBuilder(w.currentGroupID).WithLimit(100).WithCursor(w.currentCursor)
		groupUsers, err := w.roAPI.Groups().GetGroupUsers(context.Background(), builder.Build())
		if err != nil {
			w.logger.Error("Error fetching group members", zap.Error(err))
			w.pendingUserIDs = userIDs
			return nil, err
		}

		// If the group has no users, get next group
		if len(groupUsers.Data) == 0 {
			// Reset state
			w.currentGroupID = 0
			w.currentCursor = ""

			// Get next group
			nextGroup, err := w.db.Model().Group().GetGroupToScan(context.Background())
			if err != nil {
				if !errors.Is(err, sql.ErrNoRows) {
					w.logger.Error("Error getting next group to scan", zap.Error(err))
				} else {
					w.logger.Warn("No more groups to scan", zap.Error(err))
				}
				return nil, err
			}
			w.currentGroupID = nextGroup.ID
			continue
		}

		// Extract user IDs from member list
		newUserIDs := make([]uint64, len(groupUsers.Data))
		for i, groupUser := range groupUsers.Data {
			newUserIDs[i] = groupUser.User.UserID
		}

		// Check which users have been recently processed
		existingUsers, err := w.db.Model().User().GetRecentlyProcessedUsers(context.Background(), newUserIDs)
		if err != nil {
			w.logger.Error("Error checking recently processed users", zap.Error(err))
			continue
		}

		// Add only new users to the userIDs slice
		for _, userID := range newUserIDs {
			if _, exists := existingUsers[userID]; !exists {
				userIDs = append(userIDs, userID)
			}
		}

		w.logger.Info("Fetched group users",
			zap.Uint64("groupID", w.currentGroupID),
			zap.String("cursor", w.currentCursor),
			zap.Int("totalUsers", len(groupUsers.Data)),
			zap.Int("newUsers", len(newUserIDs)-len(existingUsers)),
			zap.Int("userIDs", len(userIDs)))

		// Check if we've reached batch size
		if len(userIDs) >= w.batchSize {
			// Save state for next iteration if there are more pages
			if groupUsers.NextPageCursor != nil {
				w.currentCursor = *groupUsers.NextPageCursor
			} else {
				// Reset state if no more pages
				w.currentGroupID = 0
				w.currentCursor = ""
			}
			return userIDs, nil
		}

		// Move to next page if available
		if groupUsers.NextPageCursor == nil {
			nextGroup, err := w.db.Model().Group().GetGroupToScan(context.Background())
			if err != nil {
				if !errors.Is(err, sql.ErrNoRows) {
					w.logger.Error("Error getting next group to scan", zap.Error(err))
				} else {
					w.logger.Warn("No more groups to scan", zap.Error(err))
				}
				return nil, err
			}
			w.currentGroupID = nextGroup.ID
			w.currentCursor = ""
			continue
		}

		w.currentCursor = *groupUsers.NextPageCursor
	}
}

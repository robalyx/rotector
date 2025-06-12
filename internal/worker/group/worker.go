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
	"github.com/robalyx/rotector/internal/database/types"
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
	pendingUsers     []*types.ReviewUser
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
		pendingUsers:     make([]*types.ReviewUser, 0),
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

		// Step 2: Get group users (70%)
		w.bar.SetStepMessage("Processing group users", 70)
		w.reporter.UpdateStatus("Processing group users", 70)
		userInfos, err := w.processGroup()
		if err != nil {
			w.reporter.SetHealthy(false)
			time.Sleep(5 * time.Minute)
			continue
		}

		// Step 3: Process users (90%)
		w.bar.SetStepMessage("Processing users", 90)
		w.reporter.UpdateStatus("Processing users", 90)
		w.userChecker.ProcessUsers(userInfos[:w.batchSize], nil)

		// Step 4: Prepare for next batch
		w.pendingUsers = userInfos[w.batchSize:]

		// Step 6: Completed (100%)
		w.bar.SetStepMessage("Completed", 100)
		w.reporter.UpdateStatus("Completed", 100)

		// Short pause before next iteration
		time.Sleep(1 * time.Second)
	}
}

// processGroup builds a list of validated users to check.
func (w *Worker) processGroup() ([]*types.ReviewUser, error) {
	validUsers := w.pendingUsers

	for len(validUsers) < w.batchSize {
		// Collect user IDs from current group
		userIDs, shouldContinue, err := w.collectUserIDsFromGroup()
		if err != nil {
			return nil, err
		}
		if !shouldContinue {
			break
		}

		// Fetch user info and validate
		if len(userIDs) > 0 {
			userInfos := w.userFetcher.FetchInfos(context.Background(), userIDs)
			validUsers = append(validUsers, userInfos...)

			w.logger.Info("Processed group users",
				zap.Uint64("groupID", w.currentGroupID),
				zap.String("cursor", w.currentCursor),
				zap.Int("fetchedUsers", len(userIDs)),
				zap.Int("validUsers", len(userInfos)),
				zap.Int("totalValidUsers", len(validUsers)))
		}
	}

	return validUsers, nil
}

// collectUserIDsFromGroup collects user IDs from the current group that don't exist in our system.
func (w *Worker) collectUserIDsFromGroup() ([]uint64, bool, error) {
	// Fetch group users with cursor pagination
	builder := groups.NewGroupUsersBuilder(w.currentGroupID).WithLimit(100).WithCursor(w.currentCursor)
	groupUsers, err := w.roAPI.Groups().GetGroupUsers(context.Background(), builder.Build())
	if err != nil {
		w.logger.Error("Error fetching group members", zap.Error(err))
		return nil, false, err
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
			return nil, false, err
		}
		w.currentGroupID = nextGroup.ID
		return nil, true, nil // Continue with next group
	}

	// Extract user IDs from member list
	newUserIDs := make([]uint64, len(groupUsers.Data))
	for i, groupUser := range groupUsers.Data {
		newUserIDs[i] = groupUser.User.UserID
	}

	// Check which users exist in our system
	existingUsers, err := w.db.Model().User().GetUsersByIDs(context.Background(), newUserIDs, types.UserFieldID)
	if err != nil {
		w.logger.Error("Error checking existing users", zap.Error(err))
		return nil, true, nil // Continue despite error
	}

	// Collect users that do not exist in our system
	var userIDs []uint64
	for _, userID := range newUserIDs {
		if _, exists := existingUsers[userID]; !exists {
			userIDs = append(userIDs, userID)
		}
	}

	// Update cursor for next iteration
	if groupUsers.NextPageCursor != nil {
		w.currentCursor = *groupUsers.NextPageCursor
	} else {
		// Reset state if no more pages - will get next group on next call
		w.currentGroupID = 0
		w.currentCursor = ""
	}

	return userIDs, true, nil
}

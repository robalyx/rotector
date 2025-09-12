package group

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/jaxron/roapi.go/pkg/api/resources/groups"
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

const MaxGroupsPerBatch = 10

// Worker processes group member lists by checking each member's
// status and analyzing their profiles for inappropriate content.
type Worker struct {
	db                        database.Client
	roAPI                     *api.API
	cfClient                  *cloudflare.Client
	bar                       *components.ProgressBar
	userFetcher               *fetcher.UserFetcher
	userChecker               *checker.UserChecker
	reporter                  *core.StatusReporter
	thresholdChecker          *core.ThresholdChecker
	processingCache           *core.UserProcessingCache
	pendingUsers              []*types.ReviewUser
	logger                    *zap.Logger
	batchSize                 int
	currentGroupID            int64
	currentCursor             string
	batchAttemptsWithoutFlags int
}

// New creates a new group worker.
func New(app *setup.App, bar *components.ProgressBar, logger *zap.Logger, instanceID string) *Worker {
	userFetcher := fetcher.NewUserFetcher(app, logger)
	userChecker := checker.NewUserChecker(app, userFetcher, logger)
	reporter := core.NewStatusReporter(app.StatusClient, "group", instanceID, logger)
	thresholdChecker := core.NewThresholdChecker(
		app.DB,
		app.Config.Worker.ThresholdLimits.FlaggedUsers,
		bar,
		reporter,
		logger.Named("group_worker"),
		"group worker",
	)
	processingCache := core.NewUserProcessingCache(app.RedisManager, logger)

	return &Worker{
		db:               app.DB,
		roAPI:            app.RoAPI,
		cfClient:         app.CFClient,
		bar:              bar,
		userFetcher:      userFetcher,
		userChecker:      userChecker,
		reporter:         reporter,
		thresholdChecker: thresholdChecker,
		processingCache:  processingCache,
		pendingUsers:     make([]*types.ReviewUser, 0),
		logger:           logger.Named("group_worker"),
		batchSize:        app.Config.Worker.BatchSizes.GroupUsers,
	}
}

// Start begins the group worker's main loop.
func (w *Worker) Start(ctx context.Context) {
	w.logger.Info("Group Worker started", zap.String("workerID", w.reporter.GetWorkerID()))

	w.reporter.Start(ctx)
	defer w.reporter.Stop()

	w.bar.SetTotal(100)

	for {
		// Check if context was cancelled
		if utils.ContextGuardWithLog(ctx, w.logger, "Context cancelled, stopping group worker") {
			w.bar.SetStepMessage("Shutting down", 100)
			w.reporter.UpdateStatus("Shutting down", 100)

			return
		}

		w.bar.Reset()
		w.reporter.SetHealthy(true)

		// Check flagged users threshold
		thresholdExceeded, err := w.thresholdChecker.CheckThreshold(ctx)
		if err != nil {
			if !utils.ErrorSleep(ctx, 5*time.Minute, w.logger, "group worker") {
				return
			}

			continue
		}

		if thresholdExceeded {
			continue
		}

		// Step 1: Get next group to process (10%)
		w.bar.SetStepMessage("Fetching next group to process", 10)
		w.reporter.UpdateStatus("Fetching next group to process", 10)

		if w.currentGroupID == 0 {
			if err := w.moveToNextGroup(ctx); err != nil {
				w.reporter.SetHealthy(false)

				if !utils.ErrorSleep(ctx, 5*time.Minute, w.logger, "group worker") {
					return
				}

				continue
			}
		}

		// Step 2: Get group users (70%)
		w.bar.SetStepMessage("Processing group users", 70)
		w.reporter.UpdateStatus("Processing group users", 70)

		userInfos, err := w.processGroup(ctx)
		if err != nil {
			w.reporter.SetHealthy(false)
			w.logger.Error("Error processing group users", zap.Error(err))

			if !utils.ErrorSleep(ctx, 5*time.Minute, w.logger, "group worker") {
				return
			}

			continue
		}

		// Step 3: Process users (90%)
		w.bar.SetStepMessage("Processing users", 90)
		w.reporter.UpdateStatus("Processing users", 90)

		// Process users up to batch size or all available users if less than batch size
		usersToProcess := userInfos
		if len(userInfos) > w.batchSize {
			usersToProcess = userInfos[:w.batchSize]
		}

		processResult := w.userChecker.ProcessUsers(ctx, &checker.UserCheckerParams{
			Users:                     usersToProcess,
			InappropriateOutfitFlags:  nil,
			InappropriateProfileFlags: nil,
			InappropriateFriendsFlags: nil,
			InappropriateGroupsFlags:  nil,
		})

		// Mark processed users in cache to prevent reprocessing
		var processedUserIDs []int64
		for _, user := range usersToProcess {
			processedUserIDs = append(processedUserIDs, user.ID)
		}

		if err := w.processingCache.MarkUsersProcessed(ctx, processedUserIDs); err != nil {
			w.logger.Error("Failed to mark users as processed in cache", zap.Error(err))
		}

		// Step 4: Processing completed (95%)
		w.bar.SetStepMessage("Processing completed", 95)
		w.reporter.UpdateStatus("Processing completed", 95)

		totalProblematicUsers := len(processResult.FlaggedUsers) + len(processResult.ConfirmedUsers)
		if totalProblematicUsers > 0 {
			// Reset counter when we find flagged or confirmed users
			w.batchAttemptsWithoutFlags = 0
		} else {
			// Increment counter when no users are flagged
			w.batchAttemptsWithoutFlags++
			w.logger.Info("No users flagged in this batch",
				zap.Int64("groupID", w.currentGroupID),
				zap.Int("batchAttemptsWithoutFlags", w.batchAttemptsWithoutFlags),
				zap.Int("processedUsers", len(usersToProcess)))

			// If we've had 1 batch without flags, move to next group
			if w.batchAttemptsWithoutFlags >= 1 {
				w.logger.Info("Moving to next group after 1 batch without flagged users",
					zap.Int64("currentGroupID", w.currentGroupID),
					zap.Int("batchAttemptsWithoutFlags", w.batchAttemptsWithoutFlags))

				if err := w.moveToNextGroup(ctx); err != nil {
					w.reporter.SetHealthy(false)

					if !utils.ErrorSleep(ctx, 5*time.Minute, w.logger, "group worker") {
						return
					}

					continue
				}
				// Clear pending users since we're moving to a new group
				w.pendingUsers = nil

				continue
			}
		}

		// Step 5: Prepare for next batch
		if len(userInfos) > w.batchSize {
			w.pendingUsers = userInfos[w.batchSize:]
		} else {
			w.pendingUsers = nil
		}

		// Step 6: Completed (100%)
		w.bar.SetStepMessage("Completed", 100)
		w.reporter.UpdateStatus("Completed", 100)

		// Short pause before next iteration
		if !utils.IntervalSleep(ctx, 1*time.Second, w.logger, "group worker") {
			return
		}
	}
}

// processGroup builds a list of validated users to check.
func (w *Worker) processGroup(ctx context.Context) ([]*types.ReviewUser, error) {
	validUsers := w.pendingUsers
	groupsAttempted := 0

	for len(validUsers) < w.batchSize && groupsAttempted < MaxGroupsPerBatch {
		// Collect user IDs from current group
		userIDs, shouldContinue, err := w.collectUserIDsFromGroup(ctx)
		if err != nil {
			return nil, err
		}

		if !shouldContinue {
			break
		}

		groupsAttempted++

		// Filter out already processed users to prevent duplicate processing
		if len(userIDs) > 0 {
			unprocessedUserIDs, err := w.processingCache.FilterProcessedUsers(ctx, userIDs)
			if err != nil {
				w.logger.Error("Error filtering processed users",
					zap.Error(err),
					zap.Int64("groupID", w.currentGroupID))

				unprocessedUserIDs = userIDs
			}

			// Fetch user info and validate
			if len(unprocessedUserIDs) > 0 {
				userInfos := w.userFetcher.FetchInfos(ctx, unprocessedUserIDs)
				validUsers = append(validUsers, userInfos...)

				w.logger.Info("Processed group users",
					zap.Int64("groupID", w.currentGroupID),
					zap.String("cursor", w.currentCursor),
					zap.Int("fetchedUsers", len(userIDs)),
					zap.Int("unprocessedUsers", len(unprocessedUserIDs)),
					zap.Int("validUsers", len(userInfos)),
					zap.Int("totalValidUsers", len(validUsers)),
					zap.Int("groupsAttempted", groupsAttempted))
			}
		}
	}

	if groupsAttempted >= MaxGroupsPerBatch && len(validUsers) < w.batchSize {
		w.logger.Warn("Reached maximum groups per batch without filling batch size",
			zap.Int("validUsers", len(validUsers)),
			zap.Int("batchSize", w.batchSize),
			zap.Int("groupsAttempted", groupsAttempted))
	}

	return validUsers, nil
}

// moveToNextGroup resets the current group state and gets the next group to scan.
func (w *Worker) moveToNextGroup(ctx context.Context) error {
	// Reset state
	w.currentGroupID = 0
	w.currentCursor = ""
	w.batchAttemptsWithoutFlags = 0

	// Get next group
	nextGroup, err := w.db.Model().Group().GetGroupToScan(ctx)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			w.logger.Error("Error getting next group to scan", zap.Error(err))
		} else {
			w.logger.Warn("No more groups to scan", zap.Error(err))
		}

		return err
	}

	w.currentGroupID = nextGroup.ID

	return nil
}

// collectUserIDsFromGroup collects user IDs from the current group that don't exist in our system.
func (w *Worker) collectUserIDsFromGroup(ctx context.Context) ([]int64, bool, error) {
	// Fetch group users with cursor pagination
	builder := groups.NewGroupUsersBuilder(w.currentGroupID).WithLimit(100).WithCursor(w.currentCursor)

	groupUsers, err := w.roAPI.Groups().GetGroupUsers(ctx, builder.Build())
	if err != nil {
		w.logger.Error("Error fetching group members, moving to next group",
			zap.Int64("groupID", w.currentGroupID),
			zap.Error(err))

		// Reset state and try next group
		if err := w.moveToNextGroup(ctx); err != nil {
			return nil, false, err
		}

		return nil, true, nil // Continue with next group
	}

	// If the group has no users, get next group
	if len(groupUsers.Data) == 0 {
		if err := w.moveToNextGroup(ctx); err != nil {
			return nil, false, err
		}

		return nil, true, nil // Continue with next group
	}

	// Extract user IDs from member list
	newUserIDs := make([]int64, len(groupUsers.Data))
	for i, groupUser := range groupUsers.Data {
		newUserIDs[i] = groupUser.User.UserID
	}

	// Check which users exist in our system
	existingUsers, err := w.db.Model().User().GetUsersByIDs(ctx, newUserIDs, types.UserFieldID)
	if err != nil {
		w.logger.Error("Error checking existing users", zap.Error(err))
		return nil, true, nil // Continue despite error
	}

	// Collect users that do not exist in our system
	var userIDs []int64

	for _, userID := range newUserIDs {
		if _, exists := existingUsers[userID]; !exists {
			userIDs = append(userIDs, userID)
		}
	}

	// Update cursor for next iteration
	if groupUsers.NextPageCursor != nil {
		w.currentCursor = *groupUsers.NextPageCursor
	} else {
		// No more pages for this group, move to next group
		if err := w.moveToNextGroup(ctx); err != nil {
			return nil, false, err
		}
	}

	return userIDs, true, nil
}

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

const (
	MaxGroupsPerBatch           = 10
	ExistingUserThreshold       = 0.90
	VirtualPageSize             = 10
	VirtualPageNoFlagsThreshold = 0.75
)

// Worker processes group member lists by checking each member's
// status and analyzing their profiles for inappropriate content.
type Worker struct {
	db               database.Client
	roAPI            *api.API
	cfClient         *cloudflare.Client
	bar              *components.ProgressBar
	userFetcher      *fetcher.UserFetcher
	userChecker      *checker.UserChecker
	reporter         *core.StatusReporter
	thresholdChecker *core.ThresholdChecker
	pendingUsers     []*types.ReviewUser
	logger           *zap.Logger
	batchSize        int
	currentGroupID   int64
	currentCursor    string
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

	return &Worker{
		db:               app.DB,
		roAPI:            app.RoAPI,
		cfClient:         app.CFClient,
		bar:              bar,
		userFetcher:      userFetcher,
		userChecker:      userChecker,
		reporter:         reporter,
		thresholdChecker: thresholdChecker,
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
			FromQueueWorker:           false,
		})

		// Mark processed users in cache to prevent reprocessing
		userCreationDates := make(map[int64]time.Time)
		for _, user := range usersToProcess {
			userCreationDates[user.ID] = user.CreatedAt
		}

		if err := w.db.Service().Cache().MarkUsersProcessed(ctx, userCreationDates); err != nil {
			w.logger.Error("Failed to mark users as processed in cache", zap.Error(err))
		}

		// Step 4: Processing completed (95%)
		w.bar.SetStepMessage("Processing completed", 95)
		w.reporter.UpdateStatus("Processing completed", 95)

		// Check if we should skip this group based on flag rate
		if w.shouldSkipGroupByFlagRate(usersToProcess, processResult) {
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

		// Add fetched users to the validation list
		if len(userIDs) > 0 {
			userInfos := w.userFetcher.FetchInfos(ctx, userIDs)

			if len(userInfos) > 0 {
				validUsers = append(validUsers, userInfos...)

				w.logger.Info("Processed group users",
					zap.Int64("groupID", w.currentGroupID),
					zap.String("cursor", w.currentCursor),
					zap.Int("unprocessedUsers", len(userIDs)),
					zap.Int("fetchedUsers", len(userInfos)),
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

	// Get next group
	nextGroup, err := w.db.Model().Group().GetGroupToScan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			w.logger.Warn("No more groups to scan")
		} else {
			w.logger.Error("Error getting next group to scan", zap.Error(err))
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
			if errors.Is(err, sql.ErrNoRows) {
				return nil, false, nil
			}

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

	// Filter out users that have been recently processed
	unprocessedIDs, err := w.db.Service().Cache().FilterProcessedUsers(ctx, userIDs)
	if err != nil {
		w.logger.Error("Error filtering processed users", zap.Error(err))
		return nil, true, nil
	}

	// Check if 90% or more users are already processed/exist (skip first page)
	if w.currentCursor != "" {
		totalUsers := len(newUserIDs)
		processedUsers := totalUsers - len(unprocessedIDs)
		existingPct := float64(processedUsers) / float64(totalUsers)

		if existingPct >= ExistingUserThreshold {
			w.logger.Info("Skipping group due to high existing user percentage",
				zap.Int64("groupID", w.currentGroupID),
				zap.Float64("existingPct", existingPct),
				zap.Int("totalUsers", totalUsers),
				zap.Int("processedUsers", processedUsers),
				zap.Int("unprocessedUsers", len(unprocessedIDs)),
				zap.Float64("threshold", ExistingUserThreshold))

			if err := w.moveToNextGroup(ctx); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return nil, false, nil
				}

				return nil, false, err
			}

			return nil, true, nil
		}
	}

	// Update cursor for next iteration
	if groupUsers.NextPageCursor != nil {
		w.currentCursor = *groupUsers.NextPageCursor
	} else {
		// No more pages for this group, move to next group
		if err := w.moveToNextGroup(ctx); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, false, nil
			}

			return nil, false, err
		}
	}

	return unprocessedIDs, true, nil
}

// shouldSkipGroupByFlagRate analyzes virtual pages to determine if group should be skipped.
// Returns true if 50% or more virtual pages have no flagged/confirmed users.
func (w *Worker) shouldSkipGroupByFlagRate(usersToProcess []*types.ReviewUser, processResult *checker.ProcessResult) bool {
	// Create map of flagged/confirmed user IDs for O(1) lookup
	flaggedUserIDs := make(map[int64]bool, len(processResult.FlaggedUsers)+len(processResult.ConfirmedUsers))
	for _, flagged := range processResult.FlaggedUsers {
		flaggedUserIDs[flagged.ID] = true
	}

	for _, confirmed := range processResult.ConfirmedUsers {
		flaggedUserIDs[confirmed.ID] = true
	}

	// Check virtual pages for flagged users
	totalVirtualPages := (len(usersToProcess) + VirtualPageSize - 1) / VirtualPageSize
	pagesWithoutFlags := 0

	for i := range totalVirtualPages {
		start := i * VirtualPageSize
		end := min(start+VirtualPageSize, len(usersToProcess))

		// Check if any users in this virtual page were flagged or confirmed
		pageHasFlags := false

		for j := start; j < end; j++ {
			if flaggedUserIDs[usersToProcess[j].ID] {
				pageHasFlags = true
				break
			}
		}

		if !pageHasFlags {
			pagesWithoutFlags++
		}
	}

	// Calculate percentage of pages without flags
	noFlagsPct := float64(pagesWithoutFlags) / float64(totalVirtualPages)

	w.logger.Info("Virtual page analysis",
		zap.Int64("groupID", w.currentGroupID),
		zap.Int("totalVirtualPages", totalVirtualPages),
		zap.Int("pagesWithoutFlags", pagesWithoutFlags),
		zap.Float64("noFlagsPct", noFlagsPct),
		zap.Int("processedUsers", len(usersToProcess)))

	// Return true if 50% or more virtual pages have no flags
	if noFlagsPct >= VirtualPageNoFlagsThreshold {
		w.logger.Info("Moving to next group due to low flag rate in virtual pages",
			zap.Int64("currentGroupID", w.currentGroupID),
			zap.Float64("noFlagsPct", noFlagsPct),
			zap.Float64("threshold", VirtualPageNoFlagsThreshold))

		return true
	}

	return false
}

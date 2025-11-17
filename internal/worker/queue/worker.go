package queue

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/roblox/checker"
	"github.com/robalyx/rotector/internal/roblox/fetcher"
	"github.com/robalyx/rotector/internal/setup"
	"github.com/robalyx/rotector/internal/tui/components"
	"github.com/robalyx/rotector/internal/worker/core"
	"github.com/robalyx/rotector/pkg/utils"
	"go.uber.org/zap"
)

// ErrNoUsersToProcess indicates that no users are available for processing.
var ErrNoUsersToProcess = errors.New("no users available for processing")

// BatchData represents the data needed for processing a batch of users.
type BatchData struct {
	ProcessIDs    []int64
	ExistingUsers map[int64]*types.ReviewUser
	OutfitFlags   map[int64]struct{}
	ProfileFlags  map[int64]struct{}
	FriendsFlags  map[int64]struct{}
	GroupsFlags   map[int64]struct{}
}

// Worker processes queued users from Cloudflare D1.
type Worker struct {
	app               *setup.App
	bar               *components.ProgressBar
	userFetcher       *fetcher.UserFetcher
	userChecker       *checker.UserChecker
	reporter          *core.StatusReporter
	logger            *zap.Logger
	batchSize         int
	processedCount    int
	windowStartTime   time.Time
	maxUsersPerWindow int
	windowDuration    time.Duration
}

// New creates a new queue worker.
func New(app *setup.App, bar *components.ProgressBar, logger *zap.Logger, instanceID string) *Worker {
	userFetcher := fetcher.NewUserFetcher(app, logger)
	userChecker := checker.NewUserChecker(app, userFetcher, logger)
	reporter := core.NewStatusReporter(app.StatusClient, "queue", instanceID, logger)

	return &Worker{
		app:               app,
		bar:               bar,
		userFetcher:       userFetcher,
		userChecker:       userChecker,
		reporter:          reporter,
		logger:            logger.Named("queue_worker"),
		batchSize:         app.Config.Worker.BatchSizes.QueueItems,
		processedCount:    0,
		windowStartTime:   time.Now(),
		maxUsersPerWindow: app.Config.Worker.QueueRateLimiting.MaxUsersPerWindow,
		windowDuration:    app.Config.Worker.QueueRateLimiting.WindowDuration,
	}
}

// Start begins the queue worker's main processing loop.
func (w *Worker) Start(ctx context.Context) {
	w.logger.Info("Queue Worker started")
	w.bar.SetTotal(100)

	// Start status reporting
	w.reporter.Start(ctx)
	defer w.reporter.Stop()

	// Cleanup IP tracking records on startup
	if err := w.app.CFClient.IPTracking.Cleanup(
		ctx,
		30*24*time.Hour, // Remove IP tracking records older than 30 days
	); err != nil {
		w.logger.Error("Failed to cleanup IP tracking", zap.Error(err))
	}

	for {
		// Check if context was cancelled
		if utils.ContextGuardWithLog(ctx, w.logger, "Context cancelled, stopping queue worker") {
			w.bar.SetStepMessage("Shutting down", 100)
			return
		}

		w.bar.Reset()

		// Check if we can process based on rate limiting
		canProcess, sleepDuration := w.canProcessBatch(w.batchSize)
		if !canProcess {
			w.bar.SetStepMessage("Waiting for rate limit", 0)
			w.logger.Debug("Waiting for rate limit window",
				zap.Duration("sleep_duration", sleepDuration))

			if utils.ContextSleep(ctx, sleepDuration) == utils.SleepCancelled {
				w.logger.Info("Context cancelled during rate limit wait, stopping queue worker")
				return
			}

			continue
		}

		// Step 1: Get next batch of unprocessed users (25%)
		w.bar.SetStepMessage("Getting next batch", 25)

		batchData, err := w.getBatchForProcessing(ctx)
		if err != nil {
			if errors.Is(err, ErrNoUsersToProcess) {
				w.bar.SetStepMessage("No items to process", 0)

				if utils.ContextSleep(ctx, 10*time.Second) == utils.SleepCancelled {
					w.logger.Info("Context cancelled during no items wait, stopping queue worker")
					return
				}

				continue
			}

			w.logger.Error("Failed to get batch for processing", zap.Error(err))

			if utils.ContextSleep(ctx, 5*time.Second) == utils.SleepCancelled {
				w.logger.Info("Context cancelled during error wait, stopping queue worker")
				return
			}

			continue
		}

		// If no users to process, skip to next batch
		if len(batchData.ProcessIDs) == 0 {
			continue
		}

		// Step 2: Fetch user info (40%)
		w.bar.SetStepMessage("Fetching user info", 40)
		userInfos := w.userFetcher.FetchInfos(ctx, batchData.ProcessIDs)

		// Step 3: Process users with checker (60%)
		w.bar.SetStepMessage("Processing users", 60)
		processResult := w.userChecker.ProcessUsers(ctx, &checker.UserCheckerParams{
			Users:                     userInfos,
			ExistingUsers:             batchData.ExistingUsers,
			InappropriateOutfitFlags:  batchData.OutfitFlags,
			InappropriateProfileFlags: batchData.ProfileFlags,
			InappropriateFriendsFlags: batchData.FriendsFlags,
			InappropriateGroupsFlags:  batchData.GroupsFlags,
			FromQueueWorker:           true,
		})

		// Step 4: Mark users as processed (75%)
		w.bar.SetStepMessage("Marking as processed", 75)

		if err := w.app.CFClient.Queue.MarkAsProcessed(ctx, batchData.ProcessIDs, processResult.FlaggedStatus); err != nil {
			w.logger.Error("Failed to mark users as processed", zap.Error(err))
		}

		// Step 5: Update IP tracking (100%)
		w.bar.SetStepMessage("Updating IP tracking", 100)

		if err := w.updateIPTrackingFlaggedStatus(
			ctx, batchData.ProcessIDs, processResult.FlaggedStatus,
		); err != nil {
			w.logger.Error("Failed to update IP tracking flagged status", zap.Error(err))
		}

		// Update processed count for rate limiting
		w.processedCount += len(batchData.ProcessIDs)

		w.logger.Info("Processed batch",
			zap.Int("total", len(batchData.ProcessIDs)),
			zap.Int("new", len(batchData.ProcessIDs)-len(batchData.ExistingUsers)),
			zap.Int("reprocessed", len(batchData.ExistingUsers)))
	}
}

// canProcessBatch checks if processing a batch would exceed rate limits.
// Returns true if processing is allowed, or false with the required wait duration.
func (w *Worker) canProcessBatch(batchSize int) (bool, time.Duration) {
	now := time.Now()

	// Reset window if expired
	if now.Sub(w.windowStartTime) >= w.windowDuration {
		w.processedCount = 0
		w.windowStartTime = now
	}

	// Check if processing this batch would exceed the limit
	if w.processedCount+batchSize > w.maxUsersPerWindow {
		waitUntil := w.windowStartTime.Add(w.windowDuration)
		waitDuration := time.Until(waitUntil)

		w.logger.Debug("Rate limit would be exceeded, waiting",
			zap.Int("processed_count", w.processedCount),
			zap.Int("batch_size", batchSize),
			zap.Int("max_users_per_window", w.maxUsersPerWindow),
			zap.Duration("wait_duration", waitDuration))

		return false, waitDuration
	}

	w.logger.Debug("Rate limit check passed",
		zap.Int("processed_count", w.processedCount),
		zap.Int("batch_size", batchSize),
		zap.Int("max_users_per_window", w.maxUsersPerWindow))

	return true, 0
}

// updateIPTrackingFlaggedStatus updates the queue_ip_tracking table for processed users.
func (w *Worker) updateIPTrackingFlaggedStatus(
	ctx context.Context, processIDs []int64, flaggedStatus map[int64]struct{},
) error {
	// Build user flagged status map
	allUserFlaggedStatus := make(map[int64]bool)

	// Add processed users with their actual flagged status
	for _, userID := range processIDs {
		_, flagged := flaggedStatus[userID]
		allUserFlaggedStatus[userID] = flagged
	}

	// Update IP tracking table if we have any users to update
	if len(allUserFlaggedStatus) > 0 {
		return w.app.CFClient.IPTracking.UpdateUserFlagged(ctx, allUserFlaggedStatus)
	}

	return nil
}

// getBatchForProcessing handles getting and preparing a batch of users for processing.
func (w *Worker) getBatchForProcessing(ctx context.Context) (*BatchData, error) {
	// Get next batch of unprocessed users
	userBatch, err := w.app.CFClient.Queue.GetNextBatch(ctx, w.batchSize)
	if err != nil {
		return nil, fmt.Errorf("failed to get next batch: %w", err)
	}

	if len(userBatch.UserIDs) == 0 {
		return nil, ErrNoUsersToProcess
	}

	// Get existing users from database
	existingUsers, err := w.app.DB.Model().User().GetUsersByIDs(
		ctx, userBatch.UserIDs, types.UserFieldAll,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to check existing users: %w", err)
	}

	// Process all users from the queue
	batchData := &BatchData{
		ProcessIDs:    make([]int64, 0, len(userBatch.UserIDs)),
		ExistingUsers: make(map[int64]*types.ReviewUser),
		OutfitFlags:   make(map[int64]struct{}),
		ProfileFlags:  make(map[int64]struct{}),
		FriendsFlags:  make(map[int64]struct{}),
		GroupsFlags:   make(map[int64]struct{}),
	}

	for _, id := range userBatch.UserIDs {
		// Add all users to processing list
		batchData.ProcessIDs = append(batchData.ProcessIDs, id)

		// Track existing users for reprocessing detection
		if existingUser, exists := existingUsers[id]; exists {
			batchData.ExistingUsers[id] = existingUser
			w.logger.Debug("Reprocessing existing user",
				zap.Int64("userID", id),
				zap.String("currentStatus", existingUser.Status.String()))
		}

		// Set flag types based on queue flags
		if _, exists := userBatch.InappropriateOutfitFlags[id]; exists {
			batchData.OutfitFlags[id] = struct{}{}
		}

		if _, exists := userBatch.InappropriateProfileFlags[id]; exists {
			batchData.ProfileFlags[id] = struct{}{}
		}

		if _, exists := userBatch.InappropriateFriendsFlags[id]; exists {
			batchData.FriendsFlags[id] = struct{}{}
		}

		if _, exists := userBatch.InappropriateGroupsFlags[id]; exists {
			batchData.GroupsFlags[id] = struct{}{}
		}
	}

	return batchData, nil
}

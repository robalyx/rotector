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

const (
	MaxWaitTime       = 5 * time.Minute  // Maximum time to wait before processing
	CheckInterval     = 10 * time.Second // How often to check conditions when waiting
	MinBatchThreshold = 0.5              // Process when queue has at least 50% of batch size
)

// ErrNoUsersToProcess indicates that no users are available for processing.
var ErrNoUsersToProcess = errors.New("no users available for processing")

// BatchData represents the data needed for processing a batch of users.
type BatchData struct {
	ProcessIDs     []int64
	SkipAndFlagIDs []int64
	OutfitFlags    map[int64]struct{}
	ProfileFlags   map[int64]struct{}
	FriendsFlags   map[int64]struct{}
	GroupsFlags    map[int64]struct{}
}

// Worker processes queued users from Cloudflare D1.
type Worker struct {
	app             *setup.App
	bar             *components.ProgressBar
	userFetcher     *fetcher.UserFetcher
	userChecker     *checker.UserChecker
	reporter        *core.StatusReporter
	logger          *zap.Logger
	batchSize       int
	lastProcessTime time.Time
}

// New creates a new queue worker.
func New(app *setup.App, bar *components.ProgressBar, logger *zap.Logger, instanceID string) *Worker {
	userFetcher := fetcher.NewUserFetcher(app, logger)
	userChecker := checker.NewUserChecker(app, userFetcher, logger)
	reporter := core.NewStatusReporter(app.StatusClient, "queue", instanceID, logger)

	return &Worker{
		app:         app,
		bar:         bar,
		userFetcher: userFetcher,
		userChecker: userChecker,
		reporter:    reporter,
		logger:      logger.Named("queue_worker"),
		batchSize:   app.Config.Worker.BatchSizes.QueueItems,
	}
}

// Start begins the queue worker's main processing loop.
func (w *Worker) Start(ctx context.Context) {
	w.logger.Info("Queue Worker started")
	w.bar.SetTotal(100)

	// Start status reporting
	w.reporter.Start(ctx)
	defer w.reporter.Stop()

	// Cleanup queue on startup
	if err := w.app.D1Client.Queue.Cleanup(
		ctx,
		1*time.Hour,    // Reset items stuck processing for 1 hour
		7*24*time.Hour, // Remove processed items older than 7 days
	); err != nil {
		w.logger.Error("Failed to cleanup queue", zap.Error(err))
	}

	// Cleanup IP tracking records on startup
	if err := w.app.D1Client.IPTracking.Cleanup(
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

		// Check if we should process based on batching conditions
		shouldProcess, sleepDuration := w.shouldProcessBatch(ctx)
		if !shouldProcess {
			w.bar.SetStepMessage("Waiting for batch conditions", 0)
			w.logger.Debug("Waiting for batch conditions",
				zap.Duration("sleep_duration", sleepDuration))

			if utils.ContextSleep(ctx, sleepDuration) == utils.SleepCancelled {
				w.logger.Info("Context cancelled during wait, stopping queue worker")
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

		// Update last process time since we're about to process
		w.lastProcessTime = time.Now()

		// Step 2: Fetch user info (40%)
		w.bar.SetStepMessage("Fetching user info", 40)
		userInfos := w.userFetcher.FetchInfos(ctx, batchData.ProcessIDs)

		// Step 3: Process users with checker (60%)
		w.bar.SetStepMessage("Processing users", 60)
		processResult := w.userChecker.ProcessUsers(ctx, &checker.UserCheckerParams{
			Users:                     userInfos,
			InappropriateOutfitFlags:  batchData.OutfitFlags,
			InappropriateProfileFlags: batchData.ProfileFlags,
			InappropriateFriendsFlags: batchData.FriendsFlags,
			InappropriateGroupsFlags:  batchData.GroupsFlags,
		})

		// Step 4: Mark users as processed (75%)
		w.bar.SetStepMessage("Marking as processed", 75)

		if err := w.app.D1Client.Queue.MarkAsProcessed(ctx, batchData.ProcessIDs, processResult.FlaggedStatus); err != nil {
			w.logger.Error("Failed to mark users as processed", zap.Error(err))
		}

		// Step 5: Update IP tracking (100%)
		w.bar.SetStepMessage("Updating IP tracking", 100)

		if err := w.updateIPTrackingFlaggedStatus(
			ctx, batchData.ProcessIDs, processResult.FlaggedStatus, batchData.SkipAndFlagIDs,
		); err != nil {
			w.logger.Error("Failed to update IP tracking flagged status", zap.Error(err))
		}

		w.logger.Info("Processed batch",
			zap.Int("total", len(batchData.ProcessIDs)+len(batchData.SkipAndFlagIDs)),
			zap.Int("processed", len(batchData.ProcessIDs)),
			zap.Int("skippedAndFlagged", len(batchData.SkipAndFlagIDs)))
	}
}

// shouldProcessBatch determines if we should process a batch based on queue size and timing conditions.
func (w *Worker) shouldProcessBatch(ctx context.Context) (bool, time.Duration) {
	// Check how much time has passed since last processing
	timeSinceLastProcess := time.Since(w.lastProcessTime)
	if timeSinceLastProcess >= MaxWaitTime {
		w.logger.Debug("Processing due to time threshold",
			zap.Duration("time_since_last_process", timeSinceLastProcess))

		return true, 0
	}

	// Check queue stats to see how many items are available
	stats, err := w.app.D1Client.Queue.GetStats(ctx)
	if err != nil {
		w.logger.Error("Failed to get queue stats, proceeding with processing", zap.Error(err))
		return true, 0
	}

	minBatchSize := int(float64(w.batchSize) * MinBatchThreshold)
	if stats.Unprocessed >= minBatchSize {
		w.logger.Debug("Processing due to queue size threshold",
			zap.Int("unprocessed", stats.Unprocessed),
			zap.Int("min_batch_size", minBatchSize))

		return true, 0
	}

	// Calculate remaining wait time
	remainingWaitTime := MaxWaitTime - timeSinceLastProcess
	nextCheckTime := min(CheckInterval, remainingWaitTime)

	w.logger.Debug("Batch conditions not met, waiting",
		zap.Int("unprocessed", stats.Unprocessed),
		zap.Int("min_batch_size", minBatchSize),
		zap.Duration("time_since_last_process", timeSinceLastProcess),
		zap.Duration("remaining_wait_time", remainingWaitTime),
		zap.Duration("next_check_in", nextCheckTime))

	return false, nextCheckTime
}

// updateIPTrackingFlaggedStatus updates the  queue_ip_tracking table for processed and skipped users.
func (w *Worker) updateIPTrackingFlaggedStatus(
	ctx context.Context, processIDs []int64, flaggedStatus map[int64]struct{}, skipAndFlagIDs []int64,
) error {
	// Combine all user IDs and their flagged status
	allUserFlaggedStatus := make(map[int64]bool)

	// Add processed users with their actual flagged status
	for _, userID := range processIDs {
		_, flagged := flaggedStatus[userID]
		allUserFlaggedStatus[userID] = flagged
	}

	// Add skipped users (they are always flagged since they exist in database)
	for _, userID := range skipAndFlagIDs {
		allUserFlaggedStatus[userID] = true
	}

	// Update IP tracking table if we have any users to update
	if len(allUserFlaggedStatus) > 0 {
		return w.app.D1Client.IPTracking.UpdateUserFlagged(ctx, allUserFlaggedStatus)
	}

	return nil
}

// getBatchForProcessing handles getting and preparing a batch of users for processing.
func (w *Worker) getBatchForProcessing(ctx context.Context) (*BatchData, error) {
	// Get next batch of unprocessed users
	userBatch, err := w.app.D1Client.Queue.GetNextBatch(ctx, w.batchSize)
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

	// Separate users into different processing groups
	batchData := &BatchData{
		ProcessIDs:     make([]int64, 0),
		SkipAndFlagIDs: make([]int64, 0),
		OutfitFlags:    make(map[int64]struct{}),
		ProfileFlags:   make(map[int64]struct{}),
		FriendsFlags:   make(map[int64]struct{}),
		GroupsFlags:    make(map[int64]struct{}),
	}

	existingFlaggedUsers := make(map[int64]*types.ReviewUser)

	for _, id := range userBatch.UserIDs {
		// If user exists in database, mark as processed and flagged
		if existingUser, exists := existingUsers[id]; exists {
			batchData.SkipAndFlagIDs = append(batchData.SkipAndFlagIDs, id)
			existingFlaggedUsers[id] = existingUser
			w.logger.Debug("Skipping user - already in database (will flag)",
				zap.Int64("userID", id))

			continue
		}

		// Otherwise, this user needs processing
		batchData.ProcessIDs = append(batchData.ProcessIDs, id)
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

	// Mark and save users that should be flagged
	if len(batchData.SkipAndFlagIDs) > 0 {
		flaggedMap := make(map[int64]struct{})
		for _, id := range batchData.SkipAndFlagIDs {
			flaggedMap[id] = struct{}{}
		}

		// Mark users as processed and flagged
		if err := w.app.D1Client.Queue.MarkAsProcessed(ctx, batchData.SkipAndFlagIDs, flaggedMap); err != nil {
			w.logger.Error("Failed to mark users as processed and flagged", zap.Error(err))
		}
	}

	return batchData, nil
}

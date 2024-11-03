package queue

import (
	"context"
	"fmt"
	"time"

	"github.com/bytedance/sonic"
	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/openai/openai-go"
	"github.com/rotector/rotector/internal/common/checker"
	"github.com/rotector/rotector/internal/common/database"
	"github.com/rotector/rotector/internal/common/fetcher"
	"github.com/rotector/rotector/internal/common/progress"
	"github.com/rotector/rotector/internal/common/queue"
	"go.uber.org/zap"
)

const (
	// SkipRecentlyUpdatedUserDuration is the duration to skip a user that was recently updated.
	SkipRecentlyUpdatedUserDuration = 5 * time.Minute

	// BatchSize is the number of items to process in each batch.
	BatchSize = 10

	// Priority weights for processing.
	HighPriorityWeight   = 0.6
	NormalPriorityWeight = 0.3
	LowPriorityWeight    = 0.1
)

// ProcessWorker represents a worker that processes queued items.
type ProcessWorker struct {
	db          *database.Database
	roAPI       *api.API
	bar         *progress.Bar
	userFetcher *fetcher.UserFetcher
	userChecker *checker.UserChecker
	queue       *queue.Manager
	logger      *zap.Logger
}

// NewProcessWorker creates a new process worker instance.
func NewProcessWorker(
	db *database.Database,
	openaiClient *openai.Client,
	roAPI *api.API,
	queue *queue.Manager,
	bar *progress.Bar,
	logger *zap.Logger,
) *ProcessWorker {
	userFetcher := fetcher.NewUserFetcher(roAPI, logger)
	userChecker := checker.NewUserChecker(db, bar, roAPI, openaiClient, userFetcher, logger)

	return &ProcessWorker{
		db:          db,
		roAPI:       roAPI,
		bar:         bar,
		userFetcher: userFetcher,
		userChecker: userChecker,
		queue:       queue,
		logger:      logger,
	}
}

// Start begins the process worker's main loop.
func (w *ProcessWorker) Start() {
	w.logger.Info("Queue Process Worker started")
	w.bar.SetTotal(100)

	for {
		w.bar.Reset()

		// Step 1: Get items from queues based on priority weights (10%)
		w.bar.SetStepMessage("Fetching queue items")
		items, err := w.getItemsFromQueues()
		if err != nil {
			w.logger.Error("Error getting items from queues", zap.Error(err))
			time.Sleep(5 * time.Second)
			continue
		}
		w.bar.Increment(10)

		// If no items in queues, wait before next batch
		if len(items) == 0 {
			w.logger.Info("No items in queues, waiting...")
			time.Sleep(1 * time.Second)
			continue
		}

		// Update status to "Processing" for all items
		for _, item := range items {
			if err := w.queue.SetQueueInfo(context.Background(), item.UserID, queue.StatusProcessing, item.Priority, 0); err != nil {
				w.logger.Error("Failed to update queue status to processing",
					zap.Error(err),
					zap.Uint64("userID", item.UserID))
			}
		}

		// Step 2: Fetch user info (15%)
		w.bar.SetStepMessage("Fetching user info")
		userIDs := make([]uint64, len(items))
		for i, item := range items {
			userIDs[i] = item.UserID
		}
		userInfos := w.userFetcher.FetchInfos(userIDs)
		w.bar.Increment(15)

		// Step 3: Process users (60%)
		w.userChecker.ProcessUsers(userInfos)

		// Step 4: Update status and cleanup (15%)
		w.bar.SetStepMessage("Cleaning up queue")
		for _, item := range items {
			// Set status to Complete
			if err := w.queue.SetQueueInfo(context.Background(), item.UserID, queue.StatusComplete, item.Priority, 0); err != nil {
				w.logger.Error("Failed to update queue status to complete",
					zap.Error(err),
					zap.Uint64("userID", item.UserID))
			}

			// Remove from queue
			key := fmt.Sprintf("queue:%s_priority", item.Priority)
			if err := w.queue.RemoveQueueItem(context.Background(), key, item); err != nil {
				w.logger.Error("Failed to remove item from queue",
					zap.Error(err),
					zap.Uint64("userID", item.UserID))
			}
		}
		w.bar.Increment(15)

		// Wait before next iteration
		time.Sleep(1 * time.Minute)
	}
}

// getItemsFromQueues fetches items from all queues based on priority weights.
func (w *ProcessWorker) getItemsFromQueues() ([]*queue.Item, error) {
	var allItems []*queue.Item
	addedUsers := make(map[uint64]bool)

	// Calculate batch sizes based on weights
	highBatch := int(float64(BatchSize) * HighPriorityWeight)
	normalBatch := int(float64(BatchSize) * NormalPriorityWeight)
	lowBatch := int(float64(BatchSize) * LowPriorityWeight)

	// Get items from each queue
	priorities := []struct {
		priority string
		batch    int
	}{
		{queue.HighPriority, highBatch},
		{queue.NormalPriority, normalBatch},
		{queue.LowPriority, lowBatch},
	}

	for _, p := range priorities {
		key := fmt.Sprintf("queue:%s_priority", p.priority)

		// Get items from sorted set
		result, err := w.queue.GetQueueItems(context.Background(), key, p.batch)
		if err != nil {
			return nil, fmt.Errorf("failed to get items from queue: %w", err)
		}

		// Parse items
		for _, itemJSON := range result {
			var item queue.Item
			if err := sonic.Unmarshal([]byte(itemJSON), &item); err != nil {
				w.logger.Error("Failed to unmarshal queue item", zap.Error(err))
				continue
			}

			// Skip if user ID already exists in our collection
			if addedUsers[item.UserID] {
				w.logger.Debug("Skipping duplicate user in queue",
					zap.Uint64("userID", item.UserID),
					zap.String("priority", p.priority))
				continue
			}

			// Skip if item has been aborted
			if w.queue.IsAborted(context.Background(), item.UserID) {
				// Remove from queue since it's aborted
				if err := w.queue.RemoveQueueItem(context.Background(), key, &item); err != nil {
					w.logger.Error("Failed to remove aborted item from queue",
						zap.Error(err),
						zap.Uint64("userID", item.UserID))
				}
				continue
			}

			// Update queue position for this item
			if err := w.queue.SetQueueInfo(context.Background(), item.UserID, queue.StatusPending, item.Priority, len(allItems)+1); err != nil {
				w.logger.Error("Failed to update queue position",
					zap.Error(err),
					zap.Uint64("userID", item.UserID))
			}

			// Check if user was recently updated
			if item.CheckExists && w.shouldSkipItem(key, &item) {
				continue
			}

			// Mark this user ID as seen
			addedUsers[item.UserID] = true
			allItems = append(allItems, &item)
		}
	}

	return allItems, nil
}

// shouldSkipItem checks if a user was recently updated and removes them from the queue if so.
func (w *ProcessWorker) shouldSkipItem(key string, item *queue.Item) bool {
	flaggedUser, err := w.db.Users().GetFlaggedUserByID(item.UserID)
	if err == nil && time.Since(flaggedUser.LastUpdated) < SkipRecentlyUpdatedUserDuration {
		// User was recently updated, remove from queue and skip
		if err := w.queue.RemoveQueueItem(context.Background(), key, item); err != nil {
			w.logger.Error("Failed to remove recently updated item from queue",
				zap.Error(err),
				zap.Uint64("userID", item.UserID))
			return true
		}

		// Set status to Skipped
		if err := w.queue.SetQueueInfo(context.Background(), item.UserID, queue.StatusSkipped, item.Priority, 0); err != nil {
			w.logger.Error("Failed to update queue status to skipped",
				zap.Error(err),
				zap.Uint64("userID", item.UserID))
			return true
		}

		w.logger.Info("Skipped recently updated user",
			zap.Uint64("userID", item.UserID),
			zap.Time("lastUpdated", flaggedUser.LastUpdated))
		return true
	}

	return false
}

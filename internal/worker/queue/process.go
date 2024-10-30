package queue

import (
	"encoding/json"
	"fmt"
	"time"

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
func NewProcessWorker(db *database.Database, openaiClient *openai.Client, roAPI *api.API, queue *queue.Manager, bar *progress.Bar, logger *zap.Logger) *ProcessWorker {
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

		// Step 1: Get items from queues based on priority weights (25%)
		w.bar.SetStepMessage("Fetching queue items")
		items, err := w.getItemsFromQueues()
		if err != nil {
			w.logger.Error("Error getting items from queues", zap.Error(err))
			time.Sleep(5 * time.Second)
			continue
		}
		w.bar.Increment(25)

		// If no items in queues, wait before next batch
		if len(items) == 0 {
			w.logger.Info("No items in queues, waiting...")
			time.Sleep(1 * time.Second)
			continue
		}

		// Step 2: Fetch user info (25%)
		w.bar.SetStepMessage("Fetching user info")
		userIDs := make([]uint64, len(items))
		for i, item := range items {
			userIDs[i] = item.UserID
		}
		userInfos := w.userFetcher.FetchInfos(userIDs)
		w.bar.Increment(25)

		// Step 3: Process users (25%)
		w.bar.SetStepMessage("Processing users")
		w.userChecker.ProcessUsers(userInfos)
		w.bar.Increment(25)

		// Step 4: Remove processed items from queue (25%)
		w.bar.SetStepMessage("Cleaning up queue")
		w.removeProcessedItems(items)
		w.bar.Increment(25)

		// Wait before next iteration
		time.Sleep(1 * time.Minute)
	}
}

// getItemsFromQueues fetches items from all queues based on priority weights.
func (w *ProcessWorker) getItemsFromQueues() ([]*queue.Item, error) {
	var allItems []*queue.Item

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
		result, err := w.queue.GetQueueItems(key, p.batch)
		if err != nil {
			return nil, fmt.Errorf("failed to get items from queue: %w", err)
		}

		// Parse items
		for _, itemJSON := range result {
			var item queue.Item
			if err := json.Unmarshal([]byte(itemJSON), &item); err != nil {
				w.logger.Error("Failed to unmarshal queue item", zap.Error(err))
				continue
			}
			allItems = append(allItems, &item)
		}
	}

	return allItems, nil
}

// removeProcessedItems removes all processed items from their respective queues.
func (w *ProcessWorker) removeProcessedItems(items []*queue.Item) {
	for _, item := range items {
		key := fmt.Sprintf("queue:%s_priority", item.Priority)

		if err := w.queue.RemoveQueueItem(key, item); err != nil {
			w.logger.Error("Failed to remove processed item from queue",
				zap.Error(err),
				zap.Uint64("userID", item.UserID))
		}
	}
}

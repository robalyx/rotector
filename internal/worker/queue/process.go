package queue

import (
	"context"
	"fmt"
	"time"

	"github.com/bytedance/sonic"
	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/openai/openai-go"
	"github.com/redis/rueidis"
	"github.com/rotector/rotector/internal/common/checker"
	"github.com/rotector/rotector/internal/common/database"
	"github.com/rotector/rotector/internal/common/fetcher"
	"github.com/rotector/rotector/internal/common/progress"
	"github.com/rotector/rotector/internal/common/queue"
	"github.com/rotector/rotector/internal/common/worker"
	"go.uber.org/zap"
)

// ProcessWorker handles items in the processing queue by:
// 1. Checking different priority queues in order (high -> normal -> low)
// 2. Running AI analysis on queued users
// 3. Updating queue status and position information.
type ProcessWorker struct {
	db           *database.Database
	openAIClient *openai.Client
	roAPI        *api.API
	queue        *queue.Manager
	bar          *progress.Bar
	userFetcher  *fetcher.UserFetcher
	userChecker  *checker.UserChecker
	reporter     *worker.StatusReporter
	logger       *zap.Logger
}

// NewProcessWorker creates a ProcessWorker.
func NewProcessWorker(
	db *database.Database,
	openAIClient *openai.Client,
	roAPI *api.API,
	queue *queue.Manager,
	redisClient rueidis.Client,
	bar *progress.Bar,
	logger *zap.Logger,
) *ProcessWorker {
	userFetcher := fetcher.NewUserFetcher(roAPI, logger)
	userChecker := checker.NewUserChecker(db, bar, roAPI, openAIClient, userFetcher, logger)
	reporter := worker.NewStatusReporter(redisClient, "queue", "process", logger)

	return &ProcessWorker{
		db:           db,
		openAIClient: openAIClient,
		roAPI:        roAPI,
		queue:        queue,
		bar:          bar,
		userFetcher:  userFetcher,
		userChecker:  userChecker,
		reporter:     reporter,
		logger:       logger,
	}
}

// Start begins the process worker's main loop:
// 1. Gets items from queues in priority order
// 2. Processes each item through AI analysis
// 3. Updates queue status and position
// 4. Repeats until stopped.
func (p *ProcessWorker) Start() {
	p.logger.Info("Process Worker started", zap.String("workerID", p.reporter.GetWorkerID()))
	p.reporter.Start()
	defer p.reporter.Stop()

	p.bar.SetTotal(100)

	for {
		p.bar.Reset()

		// Step 1: Get next batch of items (20%)
		p.bar.SetStepMessage("Getting next batch", 20)
		p.reporter.UpdateStatus("Getting next batch", 20)
		items, err := p.getNextBatch()
		if err != nil {
			p.logger.Error("Error getting next batch", zap.Error(err))
			p.reporter.SetHealthy(false)
			time.Sleep(5 * time.Minute) // Wait before trying again
			continue
		}

		// If no items to process, wait before checking again
		if len(items) == 0 {
			p.bar.SetStepMessage("No items to process, waiting", 0)
			p.reporter.UpdateStatus("No items to process, waiting", 0)
			time.Sleep(5 * time.Minute)
			continue
		}

		// Step 2: Process items (80%)
		p.processItems(items)
	}
}

// getNextBatch retrieves items from queues in priority order:
// 1. High priority queue first
// 2. Normal priority queue second
// 3. Low priority queue last
// Returns up to 10 items per batch.
func (p *ProcessWorker) getNextBatch() ([]*queue.Item, error) {
	ctx := context.Background()
	var items []*queue.Item

	// Check queues in priority order
	for _, priority := range []string{
		queue.HighPriority,
		queue.NormalPriority,
		queue.LowPriority,
	} {
		// Get items from current priority queue
		key := fmt.Sprintf("queue:%s_priority", priority)
		itemsJSON, err := p.queue.GetQueueItems(ctx, key, 10-len(items))
		if err != nil {
			return nil, fmt.Errorf("failed to get items from queue: %w", err)
		}

		// Parse items from JSON
		for _, itemJSON := range itemsJSON {
			var item queue.Item
			if err := sonic.Unmarshal([]byte(itemJSON), &item); err != nil {
				p.logger.Error("Failed to unmarshal queue item",
					zap.Error(err),
					zap.String("itemJSON", itemJSON))
				continue
			}

			// Skip aborted items
			if p.queue.IsAborted(ctx, item.UserID) {
				if err := p.queue.ClearQueueInfo(ctx, item.UserID); err != nil {
					p.logger.Error("Failed to clear queue info for aborted item",
						zap.Error(err),
						zap.Uint64("userID", item.UserID))
				}
				if err := p.queue.RemoveQueueItem(ctx, key, &item); err != nil {
					p.logger.Error("Failed to remove aborted item from queue",
						zap.Error(err),
						zap.Uint64("userID", item.UserID))
				}
				continue
			}

			items = append(items, &item)
		}

		// Stop if batch is full
		if len(items) >= 10 {
			break
		}
	}

	return items, nil
}

// processItems handles each queued item by:
// 1. Updating queue status to "Processing"
// 2. Fetching user information
// 3. Running AI analysis
// 4. Updating final queue status
// 5. Removing item from queue.
func (p *ProcessWorker) processItems(items []*queue.Item) {
	ctx := context.Background()
	itemCount := len(items)
	increment := 80 / itemCount

	for i, item := range items {
		progress := 20 + ((i + 1) * increment) // Start at 20% and increment for each item
		p.bar.SetStepMessage(fmt.Sprintf("Processing item %d/%d", i+1, itemCount), int64(progress))
		p.reporter.UpdateStatus(fmt.Sprintf("Processing item %d/%d", i+1, itemCount), progress)

		// Update status to processing
		if err := p.queue.SetQueueInfo(ctx, item.UserID, queue.StatusProcessing, item.Priority, 0); err != nil {
			p.logger.Error("Failed to update queue info",
				zap.Error(err),
				zap.Uint64("userID", item.UserID))
			p.reporter.SetHealthy(false)
			continue
		}

		// Fetch and process user
		userInfos := p.userFetcher.FetchInfos([]uint64{item.UserID})
		if len(userInfos) > 0 {
			failedValidationIDs := p.userChecker.ProcessUsers(userInfos)

			// If validation failed, update status back to pending
			if len(failedValidationIDs) > 0 {
				// Update status back to pending
				if err := p.queue.SetQueueInfo(
					ctx,
					item.UserID,
					queue.StatusPending,
					item.Priority,
					p.queue.GetQueueLength(ctx, item.Priority),
				); err != nil {
					p.logger.Error("Failed to update queue info",
						zap.Error(err),
						zap.Uint64("userID", item.UserID))
					p.reporter.SetHealthy(false)
				}
				continue
			}
		}

		// Update final status and remove from queue
		p.updateQueueStatus(ctx, item, queue.StatusComplete)
		p.reporter.SetHealthy(true)
	}
}

// updateQueueStatus handles the final state of a queue item by:
// 1. Setting the final status in queue info
// 2. Removing the item from its priority queue
// 3. Logging any errors that occur.
func (p *ProcessWorker) updateQueueStatus(ctx context.Context, item *queue.Item, status string) {
	// Update queue info with final status
	if err := p.queue.SetQueueInfo(ctx, item.UserID, status, item.Priority, 0); err != nil {
		p.logger.Error("Failed to update final queue info",
			zap.Error(err),
			zap.Uint64("userID", item.UserID))
	}

	// Remove item from queue
	key := fmt.Sprintf("queue:%s_priority", item.Priority)
	if err := p.queue.RemoveQueueItem(ctx, key, item); err != nil {
		p.logger.Error("Failed to remove item from queue",
			zap.Error(err),
			zap.Uint64("userID", item.UserID))
	}
}

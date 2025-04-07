package queue

import (
	"context"
	"time"

	"github.com/robalyx/rotector/internal/progress"
	"github.com/robalyx/rotector/internal/queue"
	"github.com/robalyx/rotector/internal/roblox/checker"
	"github.com/robalyx/rotector/internal/roblox/fetcher"
	"github.com/robalyx/rotector/internal/setup"
	"go.uber.org/zap"
)

// Worker processes queued users from Cloudflare D1.
type Worker struct {
	app         *setup.App
	bar         *progress.Bar
	userFetcher *fetcher.UserFetcher
	userChecker *checker.UserChecker
	d1Client    *queue.D1Client
	logger      *zap.Logger
	batchSize   int
}

// New creates a new queue worker.
func New(app *setup.App, bar *progress.Bar, logger *zap.Logger) *Worker {
	userFetcher := fetcher.NewUserFetcher(app, logger)
	userChecker := checker.NewUserChecker(app, userFetcher, logger)

	return &Worker{
		app:         app,
		bar:         bar,
		userFetcher: userFetcher,
		userChecker: userChecker,
		d1Client:    queue.NewD1Client(app, logger),
		logger:      logger.Named("queue_worker"),
		batchSize:   app.Config.Worker.BatchSizes.QueueItems,
	}
}

// Start begins the queue worker's main processing loop.
func (w *Worker) Start() {
	w.logger.Info("Queue Worker started")
	w.bar.SetTotal(100)

	// Cleanup queue on startup
	if err := w.d1Client.CleanupQueue(
		context.Background(),
		1*time.Hour,    // Reset items stuck processing for 1 hour
		7*24*time.Hour, // Remove processed items older than 7 days
	); err != nil {
		w.logger.Error("Failed to cleanup queue", zap.Error(err))
	}

	for {
		w.bar.Reset()

		// Step 1: Get next batch of unprocessed users (25%)
		w.bar.SetStepMessage("Getting next batch", 25)
		userIDs, err := w.d1Client.GetNextBatch(context.Background(), w.batchSize)
		if err != nil {
			w.logger.Error("Failed to get next batch", zap.Error(err))
			time.Sleep(5 * time.Second)
			continue
		}

		if len(userIDs) == 0 {
			w.bar.SetStepMessage("No items to process", 0)
			time.Sleep(10 * time.Second)
			continue
		}

		// Check which users have been recently processed or are confirmed
		existingUsers, err := w.app.DB.Model().User().GetRecentlyProcessedUsers(context.Background(), userIDs)
		if err != nil {
			w.logger.Error("Failed to check recently processed users", zap.Error(err))
			time.Sleep(5 * time.Second)
			continue
		}

		// Filter out confirmed and recently processed users
		unprocessedIDs := make([]uint64, 0, len(userIDs))
		for _, id := range userIDs {
			if _, exists := existingUsers[id]; !exists {
				unprocessedIDs = append(unprocessedIDs, id)
			}
		}

		if len(unprocessedIDs) == 0 {
			// Mark all as processed since they're either confirmed or recently processed
			if err := w.d1Client.MarkAsProcessed(context.Background(), userIDs, nil); err != nil {
				w.logger.Error("Failed to mark users as processed", zap.Error(err))
			}
			continue
		}

		// Step 2: Fetch user info (50%)
		w.bar.SetStepMessage("Fetching user info", 50)
		userInfos := w.userFetcher.FetchInfos(context.Background(), unprocessedIDs)

		// Step 3: Process users with checker (75%)
		w.bar.SetStepMessage("Processing users", 75)
		flaggedStatus := w.userChecker.ProcessUsers(userInfos)

		// Step 4: Mark users as processed (100%)
		w.bar.SetStepMessage("Marking as processed", 100)
		if err := w.d1Client.MarkAsProcessed(context.Background(), userIDs, flaggedStatus); err != nil {
			w.logger.Error("Failed to mark users as processed", zap.Error(err))
		}

		w.logger.Info("Processed batch",
			zap.Int("total", len(userIDs)),
			zap.Int("processed", len(unprocessedIDs)),
			zap.Int("skipped", len(userIDs)-len(unprocessedIDs)))
	}
}

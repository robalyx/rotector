package queue

import (
	"context"
	"time"

	"github.com/robalyx/rotector/internal/database/types"
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

		// Get existing users from database
		existingUsers, err := w.app.DB.Model().User().GetUsersByIDs(
			context.Background(), userIDs, types.UserFieldBasic,
		)
		if err != nil {
			w.logger.Error("Failed to check existing users", zap.Error(err))
			time.Sleep(5 * time.Second)
			continue
		}

		// Separate users into different processing groups
		processIDs := make([]uint64, 0)
		skipAndFlagIDs := make([]uint64, 0)

		for _, id := range userIDs {
			// If user exists in database, mark as processed and flagged
			if _, exists := existingUsers[id]; exists {
				skipAndFlagIDs = append(skipAndFlagIDs, id)
				w.logger.Debug("Skipping user - already in database (will flag)",
					zap.Uint64("userID", id))
				continue
			}

			// Otherwise, this user needs processing
			processIDs = append(processIDs, id)
		}

		// Mark users that should be processed and flagged
		if len(skipAndFlagIDs) > 0 {
			flaggedMap := make(map[uint64]struct{})
			for _, id := range skipAndFlagIDs {
				flaggedMap[id] = struct{}{}
			}

			if err := w.d1Client.MarkAsProcessed(context.Background(), skipAndFlagIDs, flaggedMap); err != nil {
				w.logger.Error("Failed to mark users as processed and flagged", zap.Error(err))
			}
		}

		// If no users to process, skip to next batch
		if len(processIDs) == 0 {
			continue
		}

		// Step 2: Fetch user info (50%)
		w.bar.SetStepMessage("Fetching user info", 50)
		userInfos := w.userFetcher.FetchInfos(context.Background(), processIDs)

		// Step 3: Process users with checker (75%)
		w.bar.SetStepMessage("Processing users", 75)
		flaggedStatus := w.userChecker.ProcessUsers(userInfos)

		// Step 4: Mark users as processed (100%)
		w.bar.SetStepMessage("Marking as processed", 100)
		if err := w.d1Client.MarkAsProcessed(context.Background(), processIDs, flaggedStatus); err != nil {
			w.logger.Error("Failed to mark users as processed", zap.Error(err))
		}

		w.logger.Info("Processed batch",
			zap.Int("total", len(userIDs)),
			zap.Int("processed", len(processIDs)),
			zap.Int("skippedAndFlagged", len(skipAndFlagIDs)))
	}
}

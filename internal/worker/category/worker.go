package category

import (
	"context"
	"fmt"
	"time"

	"github.com/robalyx/rotector/internal/ai"
	"github.com/robalyx/rotector/internal/database"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/setup"
	"github.com/robalyx/rotector/internal/tui/components"
	"github.com/robalyx/rotector/internal/worker/core"
	"github.com/robalyx/rotector/pkg/utils"
	"go.uber.org/zap"
)

const (
	// batchSize determines how many users to process at once.
	batchSize = 200
	// batchDelay is the wait time between processing batches.
	batchDelay = 1 * time.Second
)

// Worker classifies users without assigned categories.
type Worker struct {
	db               database.Client
	bar              *components.ProgressBar
	categoryAnalyzer *ai.CategoryAnalyzer
	reporter         *core.StatusReporter
	logger           *zap.Logger
}

// New creates a new category worker.
func New(app *setup.App, bar *components.ProgressBar, logger *zap.Logger, instanceID string) *Worker {
	reporter := core.NewStatusReporter(app.StatusClient, "category", instanceID, logger)

	return &Worker{
		db:               app.DB,
		bar:              bar,
		categoryAnalyzer: ai.NewCategoryAnalyzer(app, logger),
		reporter:         reporter,
		logger:           logger.Named("category_worker"),
	}
}

// Start begins the category worker's operation.
func (w *Worker) Start(ctx context.Context) {
	w.logger.Info("Category Worker started")

	// Start status reporting
	w.reporter.Start(ctx)
	defer w.reporter.Stop()

	w.bar.SetTotal(100)
	w.bar.SetStepMessage("Starting category classification process", 0)

	// Process users without category
	w.bar.SetStepMessage("Classifying users without category", 50)

	categoryCount, err := w.processUsersWithoutCategory(ctx)
	if err != nil {
		w.logger.Error("Failed to process users without category", zap.Error(err))
	} else {
		w.logger.Info("Processed users without category", zap.Int("count", categoryCount))
	}

	w.bar.SetStepMessage("Completed", 100)
	w.logger.Info("Category Worker completed",
		zap.Int("categorized", categoryCount))

	// Wait for shutdown signal
	w.logger.Info("Category worker finished processing, waiting for shutdown signal")
	w.bar.SetStepMessage("Waiting for shutdown", 100)
	<-ctx.Done()
	w.logger.Info("Category worker shutting down gracefully")
}

// processUsersWithoutCategory processes all users that don't have a category assigned.
func (w *Worker) processUsersWithoutCategory(ctx context.Context) (int, error) {
	totalProcessed := 0
	cursorID := int64(0)

	for !utils.ContextGuardWithLog(ctx, w.logger, "Context cancelled during batch processing") {
		// Get batch of users without category
		users, err := w.db.Model().User().GetUsersWithoutCategory(ctx, batchSize, cursorID)
		if err != nil {
			return totalProcessed, fmt.Errorf("failed to get users without category: %w", err)
		}

		// If no more users, we're done
		if len(users) == 0 {
			break
		}

		// Get user IDs for the batch
		userIDs := make([]int64, len(users))
		for i, user := range users {
			userIDs[i] = user.ID
		}

		// Get users with their reasons for classification
		usersWithReasons, err := w.db.Service().User().GetUsersByIDs(ctx, userIDs,
			types.UserFieldBasic|types.UserFieldReasons)
		if err != nil {
			w.logger.Error("Failed to get users with reasons for batch",
				zap.Error(err))

			// Advance cursor and back off to avoid hot loop
			cursorID = users[len(users)-1].ID

			utils.ContextSleep(ctx, batchDelay)

			continue
		}

		// Classify the batch
		processed, err := w.processBatch(ctx, usersWithReasons)
		if err != nil {
			w.logger.Error("Failed to process batch",
				zap.Error(err))
		} else {
			totalProcessed += processed
			w.logger.Info("Processed batch",
				zap.Int("batchSize", len(users)),
				zap.Int("processed", processed),
				zap.Int("totalProcessed", totalProcessed))
		}

		// Move to next batch
		cursorID = users[len(users)-1].ID

		// Add delay between batches
		utils.ContextSleep(ctx, batchDelay)
	}

	return totalProcessed, nil
}

// processBatch classifies a batch of users using the category analyzer.
func (w *Worker) processBatch(
	ctx context.Context, users map[int64]*types.ReviewUser,
) (int, error) {
	if len(users) == 0 {
		return 0, nil
	}

	// Classify users using AI
	categoryResults := w.categoryAnalyzer.ClassifyUsers(ctx, users, 0)

	// Save categories if any were classified
	if len(categoryResults) > 0 {
		if err := w.db.Model().User().UpdateUserCategories(ctx, categoryResults); err != nil {
			return 0, fmt.Errorf("failed to save user categories: %w", err)
		}

		return len(categoryResults), nil
	}

	return 0, nil
}

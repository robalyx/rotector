package sync

import (
	"context"
	"time"

	"github.com/robalyx/rotector/pkg/utils"
	"go.uber.org/zap"
)

// runMutualScanner continuously runs full scans for users.
func (w *Worker) runMutualScanner(ctx context.Context) {
	for {
		// Check if context was cancelled
		if utils.ContextGuardWithLog(ctx, w.logger, "Context cancelled, stopping mutual scanner") {
			return
		}

		before := time.Now().Add(-1 * time.Hour) // Scan users not checked in the last hour

		userIDs, err := w.db.Model().Sync().GetUsersForFullScan(ctx, before, 100)
		if err != nil {
			w.logger.Error("Failed to get users for full scan", zap.Error(err))

			if utils.ContextSleep(ctx, 5*time.Second) == utils.SleepCancelled {
				w.logger.Info("Context cancelled during error wait, stopping mutual scanner")
				return
			}

			continue
		}

		for _, userID := range userIDs {
			// Check if context was cancelled
			if utils.ContextGuardWithLog(ctx, w.logger, "Context cancelled during user scan, stopping mutual scanner") {
				return
			}

			if !w.scanner.ShouldScan(ctx, userID) {
				continue
			}

			_, err := w.scanner.PerformFullScan(ctx, userID)
			if err != nil {
				w.logger.Error("Failed to perform full scan",
					zap.Error(err),
					zap.Uint64("userID", userID))
			}

			// Sleep to respect rate limits
			if utils.ContextSleep(ctx, 1*time.Second) == utils.SleepCancelled {
				w.logger.Info("Context cancelled during rate limit wait, stopping mutual scanner")
				return
			}
		}

		// Sleep before next batch
		if utils.ContextSleep(ctx, 5*time.Second) == utils.SleepCancelled {
			w.logger.Info("Context cancelled during batch wait, stopping mutual scanner")
			return
		}
	}
}

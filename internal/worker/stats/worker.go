package stats

import (
	"context"
	"time"

	"github.com/redis/rueidis"
	"github.com/rotector/rotector/internal/common/progress"
	"github.com/rotector/rotector/internal/common/storage/database"
	"github.com/rotector/rotector/internal/worker/core"
	"go.uber.org/zap"
)

// Worker handles hourly statistics snapshots.
type Worker struct {
	db       *database.Client
	bar      *progress.Bar
	reporter *core.StatusReporter
	logger   *zap.Logger
}

// New creates a new stats core.
func New(db *database.Client, redisClient rueidis.Client, bar *progress.Bar, logger *zap.Logger) *Worker {
	reporter := core.NewStatusReporter(redisClient, "stats", "", logger)
	return &Worker{
		db:       db,
		bar:      bar,
		reporter: reporter,
		logger:   logger,
	}
}

// Start begins the statistics worker's main loop.
func (w *Worker) Start() {
	w.logger.Info("Statistics Worker started", zap.String("workerID", w.reporter.GetWorkerID()))
	w.reporter.Start()
	defer w.reporter.Stop()

	w.bar.SetTotal(100)

	for {
		w.bar.Reset()
		w.reporter.SetHealthy(true)

		// Step 1: Wait until the start of the next hour (0%)
		w.bar.SetStepMessage("Waiting for next hour", 0)
		w.reporter.UpdateStatus("Waiting for next hour", 0)
		nextHour := time.Now().UTC().Truncate(time.Hour).Add(time.Hour)
		time.Sleep(time.Until(nextHour))

		// Step 2: Get current stats (25%)
		w.bar.SetStepMessage("Collecting statistics", 25)
		w.reporter.UpdateStatus("Collecting statistics", 25)
		stats, err := w.db.Stats().GetCurrentStats(context.Background())
		if err != nil {
			w.logger.Error("Failed to get current stats", zap.Error(err))
			w.reporter.SetHealthy(false)
			continue
		}

		// Step 3: Save current stats (50%)
		w.bar.SetStepMessage("Saving statistics", 50)
		w.reporter.UpdateStatus("Saving statistics", 50)
		if err := w.db.Stats().SaveHourlyStats(context.Background(), stats); err != nil {
			w.logger.Error("Failed to save hourly stats", zap.Error(err))
			w.reporter.SetHealthy(false)
			continue
		}

		// Step 4: Clean up old stats (75%)
		w.bar.SetStepMessage("Cleaning up old stats", 75)
		w.reporter.UpdateStatus("Cleaning up old stats", 75)
		cutoffDate := time.Now().UTC().AddDate(0, 0, -30) // 30 days ago
		if err := w.db.Stats().PurgeOldStats(context.Background(), cutoffDate); err != nil {
			w.logger.Error("Failed to purge old stats", zap.Error(err))
			w.reporter.SetHealthy(false)
			continue
		}

		// Step 5: Completed (100%)
		w.bar.SetStepMessage("Statistics updated", 100)
		w.reporter.UpdateStatus("Statistics updated", 100)

		w.logger.Info("Hourly statistics saved successfully")
	}
}

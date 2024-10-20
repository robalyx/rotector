package stats

import (
	"context"
	"time"

	"github.com/rotector/rotector/internal/common/database"
	"github.com/rotector/rotector/internal/common/progress"
	"go.uber.org/zap"
)

// StatisticsWorker is responsible for uploading daily statistics to the database.
type StatisticsWorker struct {
	db     *database.Database
	bar    *progress.Bar
	logger *zap.Logger
}

// NewStatisticsWorker creates a new StatisticsWorker instance.
func NewStatisticsWorker(db *database.Database, bar *progress.Bar, logger *zap.Logger) *StatisticsWorker {
	return &StatisticsWorker{
		db:     db,
		bar:    bar,
		logger: logger,
	}
}

// Start begins the statistics worker's main loop.
func (w *StatisticsWorker) Start() {
	w.logger.Info("Statistics Worker started")

	for {
		w.bar.Reset()

		// Calculate the next run time
		now := time.Now()
		startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		nextRun := startOfDay.Add(24 * time.Hour)
		totalDuration := nextRun.Sub(startOfDay)
		elapsedDuration := now.Sub(startOfDay)

		w.logger.Info("Next statistics upload will run at", zap.Time("nextRun", nextRun))

		// Set the values for the progress bar
		w.bar.SetTotal(int64(totalDuration.Seconds()))
		w.bar.SetCurrent(int64(elapsedDuration.Seconds()))

		// Update the progress bar every second
		ticker := time.NewTicker(1 * time.Second)
		for remaining := time.Until(nextRun); remaining > 0; remaining = time.Until(nextRun) {
			<-ticker.C
			w.bar.Increment(1)
		}
		ticker.Stop()

		// Upload the daily statistics to the database
		if err := w.db.Stats().UploadDailyStatsToDB(context.Background()); err != nil {
			w.logger.Error("Failed to upload daily statistics", zap.Error(err))
		} else {
			w.logger.Info("Successfully uploaded daily statistics to PostgreSQL")
		}

		// Short pause before next iteration
		time.Sleep(1 * time.Second)
	}
}

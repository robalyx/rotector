package stats

import (
	"context"
	"time"

	"github.com/rotector/rotector/internal/common/setup"
	"go.uber.org/zap"
)

// StatisticsWorker is responsible for uploading daily statistics to the database.
type StatisticsWorker struct {
	setup  *setup.AppSetup
	logger *zap.Logger
}

// NewStatisticsWorker creates a new StatisticsWorker instance.
func NewStatisticsWorker(setup *setup.AppSetup, logger *zap.Logger) *StatisticsWorker {
	return &StatisticsWorker{
		setup:  setup,
		logger: logger,
	}
}

// Start begins the statistics worker's main loop.
func (w *StatisticsWorker) Start() {
	w.logger.Info("Statistics Worker started")

	for {
		// Calculate the next run time for the statistics upload
		now := time.Now()
		nextRun := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())

		// Log and sleep until the next run time
		w.logger.Info("Next statistics upload will run at", zap.Time("nextRun", nextRun))
		time.Sleep(nextRun.Sub(now))

		// Upload the daily statistics to the database
		if err := w.setup.DB.Stats().UploadDailyStatsToDB(context.Background()); err != nil {
			w.logger.Error("Failed to upload daily statistics", zap.Error(err))
		} else {
			w.logger.Info("Successfully uploaded daily statistics to PostgreSQL")
		}
	}
}

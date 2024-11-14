package stats

import (
	"context"
	"time"

	"github.com/redis/rueidis"
	"github.com/rotector/rotector/internal/common/database"
	"github.com/rotector/rotector/internal/common/progress"
	"github.com/rotector/rotector/internal/common/worker"
	"go.uber.org/zap"
)

// StatisticsWorker handles hourly statistics snapshots.
type StatisticsWorker struct {
	db       *database.Database
	bar      *progress.Bar
	reporter *worker.StatusReporter
	logger   *zap.Logger
}

// NewStatisticsWorker creates a StatisticsWorker.
func NewStatisticsWorker(db *database.Database, redisClient rueidis.Client, bar *progress.Bar, logger *zap.Logger) *StatisticsWorker {
	reporter := worker.NewStatusReporter(redisClient, "stats", "", logger)
	return &StatisticsWorker{
		db:       db,
		bar:      bar,
		reporter: reporter,
		logger:   logger,
	}
}

// Start begins the statistics worker's main loop.
func (s *StatisticsWorker) Start() {
	s.logger.Info("Statistics Worker started", zap.String("workerID", s.reporter.GetWorkerID()))
	s.reporter.Start()
	defer s.reporter.Stop()

	s.bar.SetTotal(100)

	for {
		s.bar.Reset()

		// Step 1: Wait until the start of the next hour (0%)
		s.bar.SetStepMessage("Waiting for next hour", 0)
		s.reporter.UpdateStatus("Waiting for next hour", 0)
		nextHour := time.Now().UTC().Truncate(time.Hour).Add(time.Hour)
		time.Sleep(time.Until(nextHour))

		// Step 2: Get current stats (25%)
		s.bar.SetStepMessage("Collecting statistics", 25)
		s.reporter.UpdateStatus("Collecting statistics", 25)
		stats, err := s.db.Stats().GetCurrentStats(context.Background())
		if err != nil {
			s.logger.Error("Failed to get current stats", zap.Error(err))
			s.reporter.SetHealthy(false)
			continue
		}

		// Step 3: Save current stats (50%)
		s.bar.SetStepMessage("Saving statistics", 50)
		s.reporter.UpdateStatus("Saving statistics", 50)
		if err := s.db.Stats().SaveHourlyStats(context.Background(), stats); err != nil {
			s.logger.Error("Failed to save hourly stats", zap.Error(err))
			s.reporter.SetHealthy(false)
			continue
		}

		// Step 4: Clean up old stats (75%)
		s.bar.SetStepMessage("Cleaning up old stats", 75)
		s.reporter.UpdateStatus("Cleaning up old stats", 75)
		cutoffDate := time.Now().UTC().AddDate(0, 0, -30) // 30 days ago
		if err := s.db.Stats().PurgeOldStats(context.Background(), cutoffDate); err != nil {
			s.logger.Error("Failed to purge old stats", zap.Error(err))
			s.reporter.SetHealthy(false)
			continue
		}

		// Step 5: Completed (100%)
		s.bar.SetStepMessage("Statistics updated", 100)
		s.reporter.UpdateStatus("Statistics updated", 100)
		s.reporter.SetHealthy(true)

		s.logger.Info("Hourly statistics saved successfully")
	}
}

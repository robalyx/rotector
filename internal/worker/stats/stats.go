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

// StatisticsWorker handles the daily upload of statistics from Redis to PostgreSQL.
type StatisticsWorker struct {
	db       *database.Database
	bar      *progress.Bar
	reporter *worker.StatusReporter
	logger   *zap.Logger
}

// NewStatisticsWorker creates a StatisticsWorker.
func NewStatisticsWorker(db *database.Database, redisClient rueidis.Client, bar *progress.Bar, logger *zap.Logger) *StatisticsWorker {
	reporter := worker.NewStatusReporter(redisClient, "stats", "upload", logger)

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

		// Step 1: Wait until 1 AM (0%)
		s.bar.SetStepMessage("Waiting for next upload window", 0)
		s.reporter.UpdateStatus("Waiting for next upload window", 0)
		nextRun := s.calculateNextRun()
		time.Sleep(time.Until(nextRun))

		// Step 2: Upload statistics (50%)
		s.bar.SetStepMessage("Uploading daily statistics", 50)
		s.reporter.UpdateStatus("Uploading daily statistics", 50)
		if err := s.db.Stats().UploadDailyStatsToDB(context.Background()); err != nil {
			s.logger.Error("Failed to upload daily statistics", zap.Error(err))
			s.reporter.SetHealthy(false)
			continue
		}

		// Step 3: Completed, waiting for next day (100%)
		s.bar.SetStepMessage("Upload complete, waiting for next day", 100)
		s.reporter.UpdateStatus("Upload complete, waiting for next day", 100)
		s.reporter.SetHealthy(true)

		// Wait 23 hours before checking again
		time.Sleep(23 * time.Hour)
	}
}

// calculateNextRun determines when to run the next upload by:
// 1. Finding the next 1 AM timestamp
// 2. Adding a day if it's already past 1 AM.
func (s *StatisticsWorker) calculateNextRun() time.Time {
	now := time.Now()
	nextRun := time.Date(now.Year(), now.Month(), now.Day(), 1, 0, 0, 0, now.Location())
	if now.After(nextRun) {
		nextRun = nextRun.Add(24 * time.Hour)
	}
	return nextRun
}

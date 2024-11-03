package stats

import (
	"context"
	"time"

	"github.com/rotector/rotector/internal/common/database"
	"github.com/rotector/rotector/internal/common/progress"
	"go.uber.org/zap"
)

// StatisticsWorker handles the daily upload of statistics from Redis to PostgreSQL.
type StatisticsWorker struct {
	db     *database.Database
	bar    *progress.Bar
	logger *zap.Logger
}

// NewStatisticsWorker creates a StatisticsWorker with database access for
// storing aggregated statistics.
func NewStatisticsWorker(db *database.Database, bar *progress.Bar, logger *zap.Logger) *StatisticsWorker {
	return &StatisticsWorker{
		db:     db,
		bar:    bar,
		logger: logger,
	}
}

// Start begins the statistics worker's main loop:
// 1. Waits until 1 AM to ensure all daily stats are complete
// 2. Uploads yesterday's statistics from Redis to PostgreSQL
// 3. Cleans up Redis data after successful upload
// 4. Repeats every 24 hours.
func (s *StatisticsWorker) Start() {
	s.logger.Info("Statistics Worker started")
	s.bar.SetTotal(100)

	for {
		s.bar.Reset()

		// Step 1: Wait until 1 AM (20%)
		s.bar.SetStepMessage("Waiting for next upload window")
		nextRun := s.calculateNextRun()
		time.Sleep(time.Until(nextRun))
		s.bar.Increment(20)

		// Step 2: Upload statistics (60%)
		s.bar.SetStepMessage("Uploading daily statistics")
		if err := s.db.Stats().UploadDailyStatsToDB(context.Background()); err != nil {
			s.logger.Error("Failed to upload daily statistics", zap.Error(err))
		}
		s.bar.Increment(60)

		// Step 3: Wait for next day (20%)
		s.bar.SetStepMessage("Waiting for next day")
		s.bar.Increment(20)

		// Wait 23 hours before checking again
		// This ensures we don't miss the 1 AM window
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

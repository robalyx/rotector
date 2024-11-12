package purge

import (
	"context"
	"time"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/redis/rueidis"
	"github.com/rotector/rotector/internal/common/checker"
	"github.com/rotector/rotector/internal/common/database"
	"github.com/rotector/rotector/internal/common/fetcher"
	"github.com/rotector/rotector/internal/common/progress"
	"github.com/rotector/rotector/internal/common/worker"
	"go.uber.org/zap"
)

// TrackingWorker removes old tracking data to maintain database size.
// It periodically checks for and removes stale group and user tracking entries.
type TrackingWorker struct {
	db              *database.Database
	trackingChecker *checker.TrackingChecker
	bar             *progress.Bar
	reporter        *worker.StatusReporter
	logger          *zap.Logger
}

// NewTrackingWorker creates a TrackingWorker.
func NewTrackingWorker(db *database.Database, roAPI *api.API, redisClient rueidis.Client, bar *progress.Bar, logger *zap.Logger) *TrackingWorker {
	thumbnailFetcher := fetcher.NewThumbnailFetcher(roAPI, logger)
	trackingChecker := checker.NewTrackingChecker(db, roAPI, thumbnailFetcher, logger)
	reporter := worker.NewStatusReporter(redisClient, "purge", "tracking", logger)

	return &TrackingWorker{
		db:              db,
		trackingChecker: trackingChecker,
		bar:             bar,
		reporter:        reporter,
		logger:          logger,
	}
}

// Start begins the tracking worker's main loop.
func (t *TrackingWorker) Start() {
	t.logger.Info("Tracking Worker started", zap.String("workerID", t.reporter.GetWorkerID()))
	t.reporter.Start()
	defer t.reporter.Stop()

	t.bar.SetTotal(100)

	for {
		t.bar.Reset()

		// Step 1: Check group trackings (50%)
		t.bar.SetStepMessage("Checking group trackings", 50)
		t.reporter.UpdateStatus("Checking group trackings", 50)
		if err := t.trackingChecker.CheckGroupTrackings(); err != nil {
			t.logger.Error("Error checking group trackings", zap.Error(err))
			t.reporter.SetHealthy(false)
			time.Sleep(5 * time.Minute)
			continue
		}

		// Step 2: Purge old trackings (100%)
		t.bar.SetStepMessage("Purging old trackings", 100)
		t.reporter.UpdateStatus("Purging old trackings", 100)
		cutoffDate := time.Now().AddDate(0, 0, -30) // 30 days ago
		affected, err := t.db.Tracking().PurgeOldTrackings(context.Background(), cutoffDate)
		if err != nil {
			t.logger.Error("Error purging old trackings", zap.Error(err))
			t.reporter.SetHealthy(false)
			time.Sleep(5 * time.Minute)
			continue
		}

		if affected == 0 {
			t.bar.SetStepMessage("No old trackings to purge, waiting", 0)
			t.reporter.UpdateStatus("No old trackings to purge, waiting", 0)
			t.logger.Info("No old trackings to purge, waiting", zap.Time("cutoffDate", cutoffDate))
			time.Sleep(1 * time.Hour)
			continue
		}

		// Log results
		t.logger.Info("Purged old trackings",
			zap.Int("affected", affected),
			zap.Time("cutoffDate", cutoffDate))

		// Reset health status for next iteration
		t.reporter.SetHealthy(true)

		// Short pause before next iteration
		time.Sleep(1 * time.Second)
	}
}

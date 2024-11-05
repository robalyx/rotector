package purge

import (
	"time"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/rotector/rotector/internal/common/checker"
	"github.com/rotector/rotector/internal/common/database"
	"github.com/rotector/rotector/internal/common/fetcher"
	"github.com/rotector/rotector/internal/common/progress"
	"go.uber.org/zap"
)

// TrackingWorker removes old tracking data to maintain database size.
// It periodically checks for and removes stale group and user tracking entries.
type TrackingWorker struct {
	db              *database.Database
	trackingChecker *checker.TrackingChecker
	bar             *progress.Bar
	logger          *zap.Logger
}

// NewTrackingWorker creates a TrackingWorker with database access for
// cleaning up old tracking data.
func NewTrackingWorker(db *database.Database, roAPI *api.API, bar *progress.Bar, logger *zap.Logger) *TrackingWorker {
	thumbnailFetcher := fetcher.NewThumbnailFetcher(roAPI, logger)
	trackingChecker := checker.NewTrackingChecker(db, roAPI, thumbnailFetcher, logger)

	return &TrackingWorker{
		db:              db,
		trackingChecker: trackingChecker,
		bar:             bar,
		logger:          logger,
	}
}

// Start begins the tracking worker's main loop:
// 1. Checks for groups with enough confirmed users to flag
// 2. Removes tracking data older than 30 days
// 3. Repeats every hour.
func (t *TrackingWorker) Start() {
	t.logger.Info("Tracking Worker started")
	t.bar.SetTotal(100)

	for {
		t.bar.Reset()

		// Step 1: Check group trackings (50%)
		t.bar.SetStepMessage("Checking group trackings")
		if err := t.trackingChecker.CheckGroupTrackings(); err != nil {
			t.logger.Error("Error checking group trackings", zap.Error(err))
		}
		t.bar.Increment(50)

		// Step 3: Purge old trackings (50%)
		t.bar.SetStepMessage("Purging old trackings")
		cutoffDate := time.Now().AddDate(0, 0, -30) // 30 days ago
		affected, err := t.db.Tracking().PurgeOldTrackings(cutoffDate)
		if err != nil {
			t.logger.Error("Error purging old trackings", zap.Error(err))
		}
		t.bar.Increment(50)

		if affected == 0 {
			t.bar.SetStepMessage("No old trackings to purge, waiting")
			t.logger.Info("No old trackings to purge, waiting", zap.Time("cutoffDate", cutoffDate))
			time.Sleep(1 * time.Hour)
			continue
		}

		// Log results
		t.logger.Info("Purged old trackings",
			zap.Int("affected", affected),
			zap.Time("cutoffDate", cutoffDate))

		// Short pause before next iteration
		time.Sleep(1 * time.Second)
	}
}

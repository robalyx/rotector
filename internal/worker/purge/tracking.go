package purge

import (
	"fmt"
	"time"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/rotector/rotector/internal/common/checker"
	"github.com/rotector/rotector/internal/common/database"
	"github.com/rotector/rotector/internal/common/fetcher"
	"github.com/rotector/rotector/internal/common/progress"
	"go.uber.org/zap"
)

const (
	DefaultPurgeCutoffDays = 14
	BatchSize              = 1000
	PurgeInterval          = 1 * time.Hour
)

// TrackingWorker represents a purge worker that removes old tracking entries.
type TrackingWorker struct {
	db              *database.Database
	bar             *progress.Bar
	trackingChecker *checker.TrackingChecker
	logger          *zap.Logger
}

// NewTrackingWorker creates a new purge worker instance.
func NewTrackingWorker(db *database.Database, roAPI *api.API, bar *progress.Bar, logger *zap.Logger) *TrackingWorker {
	thumbnailFetcher := fetcher.NewThumbnailFetcher(roAPI, logger)
	trackingChecker := checker.NewTrackingChecker(db, roAPI, thumbnailFetcher, logger)

	return &TrackingWorker{
		db:              db,
		bar:             bar,
		trackingChecker: trackingChecker,
		logger:          logger,
	}
}

// Start begins the purge worker's main loop.
func (p *TrackingWorker) Start() {
	p.logger.Info("Tracking Purge Worker started")

	for {
		nextRun := time.Now().Add(PurgeInterval)

		// Perform the purge operations
		p.performPurge()

		// Update progress bar until next run
		p.updateProgressUntilNextRun(nextRun)
	}
}

// performPurge executes the purge operations for group member and user network trackings.
func (p *TrackingWorker) performPurge() {
	p.bar.SetTotal(100)
	p.bar.Reset()

	// Step 1: Check group trackings (25%)
	p.bar.SetStepMessage("Checking group trackings")
	if err := p.trackingChecker.CheckGroupTrackings(); err != nil {
		p.logger.Error("Failed to check group trackings", zap.Error(err))
	}
	p.bar.Increment(25)

	// Step 2: Check user trackings (25%)
	p.bar.SetStepMessage("Checking user trackings")
	if err := p.trackingChecker.CheckUserTrackings(); err != nil {
		p.logger.Error("Failed to check user trackings", zap.Error(err))
	}
	p.bar.Increment(25)

	// Step 3: Purge old group member trackings (25%)
	p.bar.SetStepMessage("Purging old group member trackings")
	if err := p.purgeGroupMemberTrackings(); err != nil {
		p.logger.Error("Failed to purge group member trackings", zap.Error(err))
	}
	p.bar.Increment(25)

	// Step 4: Purge old user network trackings (25%)
	p.bar.SetStepMessage("Purging old user network trackings")
	if err := p.purgeUserNetworkTrackings(); err != nil {
		p.logger.Error("Failed to purge user network trackings", zap.Error(err))
	}
	p.bar.Increment(25)
}

// updateProgressUntilNextRun updates the progress bar until the next run time.
func (p *TrackingWorker) updateProgressUntilNextRun(nextRun time.Time) {
	p.bar.Reset()
	totalDuration := PurgeInterval
	p.bar.SetTotal(int64(totalDuration.Seconds()))

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for remaining := time.Until(nextRun); remaining > 0; remaining = time.Until(nextRun) {
		<-ticker.C
		elapsed := totalDuration - remaining
		p.bar.SetCurrent(int64(elapsed.Seconds()))
		p.bar.SetStepMessage(fmt.Sprintf("Next purge in %s", remaining.Round(time.Second)))
	}
}

// purgeGroupMemberTrackings removes old entries from group_member_trackings.
func (p *TrackingWorker) purgeGroupMemberTrackings() error {
	// Calculate the cutoff date
	cutoffDate := time.Now().AddDate(0, 0, -DefaultPurgeCutoffDays)

	for {
		// Purge old group member trackings in batches
		affected, err := p.db.Tracking().PurgeOldGroupMemberTrackings(cutoffDate, BatchSize)
		if err != nil {
			return err
		}

		p.logger.Info("Purged group member trackings batch",
			zap.Int("count", affected),
			zap.Time("cutoff_date", cutoffDate))

		// If less than BatchSize rows were affected, we're done
		if affected < BatchSize {
			break
		}

		// Add a small delay between batches to reduce database load
		time.Sleep(100 * time.Millisecond)
	}

	return nil
}

// purgeUserNetworkTrackings removes old entries from user_network_trackings.
func (p *TrackingWorker) purgeUserNetworkTrackings() error {
	// Calculate the cutoff date
	cutoffDate := time.Now().AddDate(0, 0, -DefaultPurgeCutoffDays)

	for {
		// Purge old user network trackings in batches
		affected, err := p.db.Tracking().PurgeOldUserNetworkTrackings(cutoffDate, BatchSize)
		if err != nil {
			return err
		}

		p.logger.Info("Purged user network trackings batch",
			zap.Int("count", affected),
			zap.Time("cutoff_date", cutoffDate))

		// If less than BatchSize rows were affected, we're done
		if affected < BatchSize {
			break
		}

		// Add a small delay between batches to reduce database load
		time.Sleep(100 * time.Millisecond)
	}

	return nil
}

package purge

import (
	"fmt"
	"time"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/rotector/rotector/internal/common/database"
	"github.com/rotector/rotector/internal/common/progress"
	"go.uber.org/zap"
)

const (
	DefaultClearedPurgeDays = 30
	ClearedPurgeInterval    = 1 * time.Hour
	ClearedPurgeBatchSize   = 100
)

// ClearedWorker represents a purge worker that removes old cleared users.
type ClearedWorker struct {
	db     *database.Database
	roAPI  *api.API
	bar    *progress.Bar
	logger *zap.Logger
}

// NewClearedWorker creates a new cleared user purge worker instance.
func NewClearedWorker(db *database.Database, roAPI *api.API, bar *progress.Bar, logger *zap.Logger) *ClearedWorker {
	return &ClearedWorker{
		db:     db,
		roAPI:  roAPI,
		bar:    bar,
		logger: logger,
	}
}

// Start begins the cleared user purge worker's main loop.
func (p *ClearedWorker) Start() {
	p.logger.Info("Cleared User Purge Worker started")

	for {
		nextRun := time.Now().Add(ClearedPurgeInterval)

		// Perform the purge operations
		p.performPurge()

		// Update progress bar until next run
		p.updateProgressUntilNextRun(nextRun)
	}
}

// performPurge executes the purge operation for cleared users.
func (p *ClearedWorker) performPurge() {
	p.bar.SetTotal(100)
	p.bar.Reset()

	// Calculate the cutoff date for cleared users
	cutoffDate := time.Now().AddDate(0, 0, -DefaultClearedPurgeDays)

	// Get and purge old cleared users in batches
	for {
		p.bar.SetStepMessage("Purging old cleared users")

		// Get and purge a batch of cleared users
		affected, err := p.db.Users().PurgeOldClearedUsers(cutoffDate, ClearedPurgeBatchSize)
		if err != nil {
			p.logger.Error("Failed to purge cleared users", zap.Error(err))
			break
		}

		// If less than batch size was affected, we're done
		if affected < ClearedPurgeBatchSize {
			break
		}

		p.logger.Info("Purged batch of cleared users",
			zap.Int("count", affected),
			zap.Time("cutoff_date", cutoffDate))

		// Add a small delay between batches to reduce database load
		time.Sleep(100 * time.Millisecond)
	}

	p.bar.SetCurrent(100)
}

// updateProgressUntilNextRun updates the progress bar until the next run time.
func (p *ClearedWorker) updateProgressUntilNextRun(nextRun time.Time) {
	p.bar.Reset()
	totalDuration := ClearedPurgeInterval
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

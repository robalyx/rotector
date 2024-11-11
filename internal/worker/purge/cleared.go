package purge

import (
	"context"
	"time"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/rotector/rotector/internal/common/database"
	"github.com/rotector/rotector/internal/common/progress"
	"go.uber.org/zap"
)

const (
	// ClearedUsersToProcess sets how many users to process in each batch.
	// This helps control memory usage and database load.
	ClearedUsersToProcess = 200
)

// ClearedWorker removes old cleared users from the database.
// It helps maintain database size by removing users that were cleared long ago.
type ClearedWorker struct {
	db     *database.Database
	roAPI  *api.API
	bar    *progress.Bar
	logger *zap.Logger
}

// NewClearedWorker creates a ClearedWorker.
func NewClearedWorker(db *database.Database, roAPI *api.API, bar *progress.Bar, logger *zap.Logger) *ClearedWorker {
	return &ClearedWorker{
		db:     db,
		roAPI:  roAPI,
		bar:    bar,
		logger: logger,
	}
}

// Start begins the purge worker's main loop:
// 1. Finds users cleared more than 30 days ago
// 2. Removes them from the database in batches
// 3. Updates statistics
// 4. Repeats until stopped.
func (p *ClearedWorker) Start() {
	p.logger.Info("Cleared Worker started")
	p.bar.SetTotal(100)

	for {
		p.bar.Reset()

		// Step 1: Calculate cutoff date (40%)
		p.bar.SetStepMessage("Calculating cutoff date")
		cutoffDate := time.Now().AddDate(0, 0, -30)
		p.bar.Increment(40)

		// Step 2: Process users (60%)
		p.bar.SetStepMessage("Processing users")
		affected, err := p.db.Users().PurgeOldClearedUsers(context.Background(), cutoffDate, ClearedUsersToProcess)
		if err != nil {
			p.logger.Error("Error purging old cleared users", zap.Error(err))
			time.Sleep(5 * time.Minute) // Wait before trying again
			continue
		}
		p.bar.Increment(60)

		if affected == 0 {
			p.bar.SetStepMessage("No old cleared users to purge, waiting")
			p.logger.Info("No old cleared users to purge, waiting", zap.Time("cutoffDate", cutoffDate))
			time.Sleep(1 * time.Second)
			continue
		}

		// Log results
		p.logger.Info("Purged old cleared users",
			zap.Int("affected", affected),
			zap.Time("cutoffDate", cutoffDate))

		// Short pause before next iteration
		time.Sleep(1 * time.Second)
	}
}

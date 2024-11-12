package purge

import (
	"context"
	"time"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/redis/rueidis"
	"github.com/rotector/rotector/internal/common/database"
	"github.com/rotector/rotector/internal/common/progress"
	"github.com/rotector/rotector/internal/common/worker"
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
	db       *database.Database
	roAPI    *api.API
	bar      *progress.Bar
	reporter *worker.StatusReporter
	logger   *zap.Logger
}

// NewClearedWorker creates a ClearedWorker.
func NewClearedWorker(db *database.Database, roAPI *api.API, redisClient rueidis.Client, bar *progress.Bar, logger *zap.Logger) *ClearedWorker {
	reporter := worker.NewStatusReporter(redisClient, "purge", "cleared", logger)

	return &ClearedWorker{
		db:       db,
		roAPI:    roAPI,
		bar:      bar,
		reporter: reporter,
		logger:   logger,
	}
}

// Start begins the purge worker's main loop:
// 1. Finds users cleared more than 30 days ago
// 2. Removes them from the database in batches
// 3. Updates statistics
// 4. Repeats until stopped.
func (p *ClearedWorker) Start() {
	p.logger.Info("Cleared Worker started", zap.String("workerID", p.reporter.GetWorkerID()))
	p.reporter.Start()
	defer p.reporter.Stop()

	p.bar.SetTotal(100)

	for {
		p.bar.Reset()

		// Step 1: Calculate cutoff date (40%)
		p.bar.SetStepMessage("Calculating cutoff date", 40)
		p.reporter.UpdateStatus("Calculating cutoff date", 40)
		cutoffDate := time.Now().AddDate(0, 0, -30)

		// Step 2: Process users (100%)
		p.bar.SetStepMessage("Processing users", 100)
		p.reporter.UpdateStatus("Processing users", 100)
		affected, err := p.db.Users().PurgeOldClearedUsers(context.Background(), cutoffDate, ClearedUsersToProcess)
		if err != nil {
			p.logger.Error("Error purging old cleared users", zap.Error(err))
			p.reporter.SetHealthy(false)
			time.Sleep(5 * time.Minute)
			continue
		}

		if affected == 0 {
			p.bar.SetStepMessage("No old cleared users to purge, waiting", 0)
			p.reporter.UpdateStatus("No old cleared users to purge, waiting", 0)
			p.logger.Info("No old cleared users to purge, waiting", zap.Time("cutoffDate", cutoffDate))
			time.Sleep(1 * time.Second)
			continue
		}

		// Log results
		p.logger.Info("Purged old cleared users",
			zap.Int("affected", affected),
			zap.Time("cutoffDate", cutoffDate))

		// Reset health status for next iteration
		p.reporter.SetHealthy(true)

		// Short pause before next iteration
		time.Sleep(1 * time.Second)
	}
}

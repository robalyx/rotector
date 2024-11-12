package purge

import (
	"context"
	"time"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/redis/rueidis"
	"github.com/rotector/rotector/internal/common/database"
	"github.com/rotector/rotector/internal/common/fetcher"
	"github.com/rotector/rotector/internal/common/progress"
	"github.com/rotector/rotector/internal/common/worker"
	"go.uber.org/zap"
)

const (
	// PurgeUsersToProcess sets how many users to check in each batch.
	PurgeUsersToProcess = 200
)

// BannedWorker removes banned users from confirmed or flagged users.
// It periodically checks users against the Roblox API to find banned accounts.
type BannedWorker struct {
	db          *database.Database
	roAPI       *api.API
	bar         *progress.Bar
	userFetcher *fetcher.UserFetcher
	reporter    *worker.StatusReporter
	logger      *zap.Logger
}

// NewBannedWorker creates a BannedWorker.
func NewBannedWorker(db *database.Database, roAPI *api.API, redisClient rueidis.Client, bar *progress.Bar, logger *zap.Logger) *BannedWorker {
	userFetcher := fetcher.NewUserFetcher(roAPI, logger)
	reporter := worker.NewStatusReporter(redisClient, "purge", "banned", logger)

	return &BannedWorker{
		db:          db,
		roAPI:       roAPI,
		bar:         bar,
		userFetcher: userFetcher,
		reporter:    reporter,
		logger:      logger,
	}
}

// Start begins the purge worker's main loop:
// 1. Gets a batch of users to check
// 2. Checks their ban status with the Roblox API
// 3. Moves banned users to the banned_users table
// 4. Repeats until stopped.
func (p *BannedWorker) Start() {
	p.logger.Info("Purge Worker started", zap.String("workerID", p.reporter.GetWorkerID()))
	p.reporter.Start()
	defer p.reporter.Stop()

	p.bar.SetTotal(100)

	for {
		p.bar.Reset()

		// Step 1: Get batch of users to check (20%)
		p.bar.SetStepMessage("Fetching users to check", 20)
		p.reporter.UpdateStatus("Fetching users to check", 20)
		users, err := p.db.Users().GetUsersToCheck(context.Background(), PurgeUsersToProcess)
		if err != nil {
			p.logger.Error("Error getting users to check", zap.Error(err))
			p.reporter.SetHealthy(false)
			time.Sleep(5 * time.Minute)
			continue
		}

		// If no users to check, wait before next batch
		if len(users) == 0 {
			p.bar.SetStepMessage("No users to check, waiting", 0)
			p.reporter.UpdateStatus("No users to check, waiting", 0)
			p.logger.Info("No users to check, waiting before next batch")
			time.Sleep(5 * time.Minute)
			continue
		}

		// Step 2: Fetch user information (60%)
		p.bar.SetStepMessage("Fetching user information", 60)
		p.reporter.UpdateStatus("Fetching user information", 60)
		bannedUserIDs, err := p.userFetcher.FetchBannedUsers(users)
		if err != nil {
			p.logger.Error("Error fetching user information", zap.Error(err))
			p.reporter.SetHealthy(false)
			time.Sleep(5 * time.Minute)
			continue
		}

		// Step 3: Process users (100%)
		if len(bannedUserIDs) > 0 {
			p.processUsers(bannedUserIDs)
		}

		// Reset health status for next iteration
		p.reporter.SetHealthy(true)

		// Short pause before next iteration
		time.Sleep(1 * time.Second)
	}
}

// processUsers moves banned users to the banned_users table.
// It logs any errors that occur during the database update.
func (p *BannedWorker) processUsers(bannedUserIDs []uint64) {
	p.logger.Info("Processing users", zap.Any("bannedUserIDs", bannedUserIDs))

	p.bar.SetStepMessage("Removing banned users", 100)
	p.reporter.UpdateStatus("Removing banned users", 100)
	err := p.db.Users().RemoveBannedUsers(context.Background(), bannedUserIDs)
	if err != nil {
		p.logger.Error("Error removing banned users", zap.Error(err))
		p.reporter.SetHealthy(false)
		return
	}

	p.logger.Info("Finished processing users", zap.Int("count", len(bannedUserIDs)))
}

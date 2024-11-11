package purge

import (
	"context"
	"time"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/rotector/rotector/internal/common/database"
	"github.com/rotector/rotector/internal/common/fetcher"
	"github.com/rotector/rotector/internal/common/progress"
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
	logger      *zap.Logger
}

// NewBannedWorker creates a BannedWorker.
func NewBannedWorker(db *database.Database, roAPI *api.API, bar *progress.Bar, logger *zap.Logger) *BannedWorker {
	return &BannedWorker{
		db:          db,
		roAPI:       roAPI,
		bar:         bar,
		userFetcher: fetcher.NewUserFetcher(roAPI, logger),
		logger:      logger,
	}
}

// Start begins the purge worker's main loop:
// 1. Gets a batch of users to check
// 2. Checks their ban status with the Roblox API
// 3. Moves banned users to the banned_users table
// 4. Repeats until stopped.
func (p *BannedWorker) Start() {
	p.logger.Info("Purge Worker started")
	p.bar.SetTotal(100)

	for {
		p.bar.Reset()

		// Step 1: Get batch of users to check (20%)
		p.bar.SetStepMessage("Fetching users to check")
		users, err := p.db.Users().GetUsersToCheck(context.Background(), PurgeUsersToProcess)
		if err != nil {
			p.logger.Error("Error getting users to check", zap.Error(err))
			time.Sleep(5 * time.Minute) // Wait before trying again
			continue
		}
		p.bar.Increment(20)

		// If no users to check, wait before next batch
		if len(users) == 0 {
			p.bar.SetStepMessage("No users to check, waiting")
			p.logger.Info("No users to check, waiting before next batch")
			time.Sleep(5 * time.Minute)
			continue
		}

		// Step 2: Fetch user information (40%)
		p.bar.SetStepMessage("Fetching user information")
		bannedUserIDs, err := p.userFetcher.FetchBannedUsers(users)
		if err != nil {
			p.logger.Error("Error fetching user information", zap.Error(err))
			time.Sleep(5 * time.Minute) // Wait before trying again
			continue
		}
		p.bar.Increment(40)

		// Step 3: Process users (40%)
		if len(bannedUserIDs) > 0 {
			p.processUsers(bannedUserIDs)
		}

		// Short pause before next iteration
		time.Sleep(1 * time.Second)
	}
}

// processUsers moves banned users to the banned_users table.
// It logs any errors that occur during the database update.
func (p *BannedWorker) processUsers(bannedUserIDs []uint64) {
	p.logger.Info("Processing users", zap.Any("bannedUserIDs", bannedUserIDs))

	p.bar.SetStepMessage("Removing banned users")
	err := p.db.Users().RemoveBannedUsers(context.Background(), bannedUserIDs)
	if err != nil {
		p.logger.Error("Error removing banned users", zap.Error(err))
	}
	p.bar.Increment(40)

	p.logger.Info("Finished processing users", zap.Int("count", len(bannedUserIDs)))
}

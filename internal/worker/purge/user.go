package purge

import (
	"time"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/rotector/rotector/internal/common/database"
	"github.com/rotector/rotector/internal/common/fetcher"
	"github.com/rotector/rotector/internal/common/progress"
	"go.uber.org/zap"
)

const (
	PurgeUsersToProcess = 200
)

// UserWorker represents a purge worker that removes banned users from confirmed or flagged users.
type UserWorker struct {
	db          *database.Database
	roAPI       *api.API
	bar         *progress.Bar
	userFetcher *fetcher.UserFetcher
	logger      *zap.Logger
}

// NewUserWorker creates a new purge worker instance.
func NewUserWorker(db *database.Database, roAPI *api.API, bar *progress.Bar, logger *zap.Logger) *UserWorker {
	return &UserWorker{
		db:          db,
		roAPI:       roAPI,
		bar:         bar,
		userFetcher: fetcher.NewUserFetcher(roAPI, logger),
		logger:      logger,
	}
}

// Start begins the purge worker's main loop.
func (p *UserWorker) Start() {
	p.logger.Info("Purge Worker started")
	p.bar.SetTotal(100)

	for {
		p.bar.Reset()

		// Step 1: Get batch of users to check (20%)
		p.bar.SetStepMessage("Fetching users to check")
		users, err := p.db.Users().GetUsersToCheck(PurgeUsersToProcess)
		if err != nil {
			p.logger.Error("Error getting users to check", zap.Error(err))
			time.Sleep(5 * time.Minute) // Wait before trying again
			continue
		}
		p.bar.Increment(20)

		// If no users to check, wait before next batch
		if len(users) == 0 {
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

// processUsers handles the processing of a batch of users.
func (p *UserWorker) processUsers(bannedUserIDs []uint64) {
	p.logger.Info("Processing users", zap.Any("bannedUserIDs", bannedUserIDs))

	p.bar.SetStepMessage("Removing banned users")
	err := p.db.Users().RemoveBannedUsers(bannedUserIDs)
	if err != nil {
		p.logger.Error("Error removing banned users", zap.Error(err))
	}
	p.bar.Increment(40)

	p.logger.Info("Finished processing users", zap.Int("count", len(bannedUserIDs)))
}

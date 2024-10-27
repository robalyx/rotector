package ai

import (
	"context"
	"time"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/openai/openai-go"
	"github.com/rotector/rotector/internal/common/checker"
	"github.com/rotector/rotector/internal/common/database"
	"github.com/rotector/rotector/internal/common/fetcher"
	"github.com/rotector/rotector/internal/common/progress"
	"go.uber.org/zap"
)

const (
	FriendUsersToProcess = 100
)

// FriendWorker represents a friend worker that processes user friends.
type FriendWorker struct {
	db          *database.Database
	roAPI       *api.API
	bar         *progress.Bar
	userFetcher *fetcher.UserFetcher
	userChecker *checker.UserChecker
	logger      *zap.Logger
}

// NewFriendWorker creates a new friend worker instance.
func NewFriendWorker(db *database.Database, openaiClient *openai.Client, roAPI *api.API, bar *progress.Bar, logger *zap.Logger) *FriendWorker {
	userFetcher := fetcher.NewUserFetcher(roAPI, logger)
	userChecker := checker.NewUserChecker(db, bar, roAPI, openaiClient, userFetcher, logger)

	return &FriendWorker{
		db:          db,
		roAPI:       roAPI,
		bar:         bar,
		userFetcher: userFetcher,
		userChecker: userChecker,
		logger:      logger,
	}
}

// Start begins the friend worker's main loop.
func (f *FriendWorker) Start() {
	f.logger.Info("Friend Worker started")
	f.bar.SetTotal(100)

	var oldFriendIDs []uint64
	for {
		f.bar.Reset()

		// Step 1: Process friends batch (20%)
		f.bar.SetStepMessage("Processing friends batch")
		friendIDs, err := f.processFriendsBatch(oldFriendIDs)
		if err != nil {
			f.logger.Error("Error processing friends batch", zap.Error(err))
			time.Sleep(5 * time.Minute) // Wait before trying again
			continue
		}
		f.bar.Increment(20)

		// Step 2: Fetch user info (20%)
		f.bar.SetStepMessage("Fetching user info")
		userInfos := f.userFetcher.FetchInfos(friendIDs[:FriendUsersToProcess])
		f.bar.Increment(20)

		// Step 3: Process users (60%)
		f.userChecker.ProcessUsers(userInfos)

		// Step 4: Prepare for next batch
		oldFriendIDs = friendIDs[FriendUsersToProcess:]

		// Short pause before next iteration
		time.Sleep(1 * time.Second)
	}
}

// processFriendsBatch processes a batch of friends and returns the remaining friend IDs.
func (f *FriendWorker) processFriendsBatch(friendIDs []uint64) ([]uint64, error) {
	for len(friendIDs) < FriendUsersToProcess {
		// Get the next confirmed user
		user, err := f.db.Users().GetNextConfirmedUser()
		if err != nil {
			f.logger.Error("Error getting next confirmed user", zap.Error(err))
			return nil, err
		}

		// Fetch friends for the user
		friends, err := f.roAPI.Friends().GetFriends(context.Background(), user.ID)
		if err != nil {
			f.logger.Error("Error fetching friends", zap.Error(err), zap.Uint64("userID", user.ID))
			continue
		}

		// Extract friend IDs
		newFriendIDs := make([]uint64, 0, len(friends))
		for _, friend := range friends {
			if !friend.IsBanned && !friend.IsDeleted {
				newFriendIDs = append(newFriendIDs, friend.ID)
			}
		}

		// Check which users already exist in the database
		existingUsers, err := f.db.Users().CheckExistingUsers(newFriendIDs)
		if err != nil {
			f.logger.Error("Error checking existing users", zap.Error(err))
			continue
		}

		// Add only new users to the friendIDs slice
		for _, friendID := range newFriendIDs {
			if _, exists := existingUsers[friendID]; !exists {
				friendIDs = append(friendIDs, friendID)
			}
		}

		f.logger.Info("Fetched friends",
			zap.Int("totalFriends", len(friends)),
			zap.Int("newFriends", len(newFriendIDs)-len(existingUsers)),
			zap.Uint64("userID", user.ID))

		// If we have enough friends, break out of the loop
		if len(friendIDs) >= FriendUsersToProcess {
			break
		}
	}

	return friendIDs, nil
}

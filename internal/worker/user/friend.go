package user

import (
	"context"
	"time"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/openai/openai-go"
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
	db               *database.Database
	roAPI            *api.API
	bar              *progress.Bar
	aiChecker        *fetcher.AIChecker
	userFetcher      *fetcher.UserFetcher
	outfitFetcher    *fetcher.OutfitFetcher
	thumbnailFetcher *fetcher.ThumbnailFetcher
	friendFetcher    *fetcher.FriendFetcher
	logger           *zap.Logger
	groupChecker     *fetcher.GroupChecker
}

// NewFriendWorker creates a new friend worker instance.
func NewFriendWorker(db *database.Database, openaiClient *openai.Client, roAPI *api.API, bar *progress.Bar, logger *zap.Logger) *FriendWorker {
	return &FriendWorker{
		db:               db,
		roAPI:            roAPI,
		bar:              bar,
		aiChecker:        fetcher.NewAIChecker(openaiClient, logger),
		userFetcher:      fetcher.NewUserFetcher(roAPI, logger),
		outfitFetcher:    fetcher.NewOutfitFetcher(roAPI, logger),
		thumbnailFetcher: fetcher.NewThumbnailFetcher(roAPI, logger),
		friendFetcher:    fetcher.NewFriendFetcher(roAPI, logger),
		logger:           logger,
		groupChecker:     fetcher.NewGroupChecker(db, logger),
	}
}

// Start begins the friend worker's main loop.
func (w *FriendWorker) Start() {
	w.logger.Info("Friend Worker started")
	w.bar.SetTotal(100)

	var oldFriendIDs []uint64
	for {
		w.bar.Reset()

		// Step 1: Process friends batch (20%)
		friendIDs, err := w.processFriendsBatch(oldFriendIDs)
		if err != nil {
			w.logger.Error("Error processing friends batch", zap.Error(err))
			time.Sleep(5 * time.Minute) // Wait before trying again
			continue
		}
		w.bar.Increment(20)

		// Step 2: Fetch user info (20%)
		userInfos := w.userFetcher.FetchInfos(friendIDs[:FriendUsersToProcess])
		w.bar.Increment(20)

		// Step 3: Process users (60%)
		w.processUsers(userInfos[:FriendUsersToProcess])

		// Step 4: Prepare for next batch
		oldFriendIDs = friendIDs[FriendUsersToProcess:]

		// Short pause before next iteration
		time.Sleep(1 * time.Second)
	}
}

// processFriendsBatch processes a batch of friends and returns the remaining friend IDs.
func (w *FriendWorker) processFriendsBatch(friendIDs []uint64) ([]uint64, error) {
	for len(friendIDs) < FriendUsersToProcess {
		// Get the next flagged user
		user, err := w.db.Users().GetNextFlaggedUser()
		if err != nil {
			return nil, err
		}

		// Fetch friends for the user
		friends, err := w.roAPI.Friends().GetFriends(context.Background(), user.ID)
		if err != nil {
			w.logger.Error("Error fetching friends", zap.Error(err), zap.Uint64("userID", user.ID))
			continue
		}

		// Add friend IDs to the slice
		for _, friend := range friends {
			if !friend.IsBanned && !friend.IsDeleted {
				friendIDs = append(friendIDs, friend.ID)
			}
		}

		w.logger.Info("Fetched friends", zap.Int("friendIDs", len(friends)), zap.Uint64("userID", user.ID))

		// If we have enough friends, break out of the loop
		if len(friendIDs) >= FriendUsersToProcess {
			break
		}
	}

	return friendIDs, nil
}

// processUsers handles the processing of a batch of users.
func (w *FriendWorker) processUsers(userInfos []*fetcher.Info) {
	w.logger.Info("Processing users", zap.Int("userInfos", len(userInfos)))

	var flaggedUsers []*database.User
	var usersForAICheck []*fetcher.Info

	// Check if users belong to any flagged groups
	for _, userInfo := range userInfos {
		user, autoFlagged, err := w.groupChecker.CheckUserGroups(userInfo)
		if err != nil {
			w.logger.Error("Error checking user groups", zap.Error(err), zap.Uint64("userID", userInfo.ID))
			continue
		}

		if autoFlagged {
			flaggedUsers = append(flaggedUsers, user)
		} else {
			usersForAICheck = append(usersForAICheck, userInfo)
		}
	}
	w.bar.Increment(10)

	// Process remaining users with AI
	if len(usersForAICheck) > 0 {
		aiFlaggedUsers, err := w.aiChecker.CheckUsers(usersForAICheck)
		if err != nil {
			w.logger.Error("Error checking users with AI", zap.Error(err))
		} else {
			flaggedUsers = append(flaggedUsers, aiFlaggedUsers...)
		}
	}
	w.bar.Increment(10)

	// Fetch necessary data for flagged users
	flaggedUsers = w.thumbnailFetcher.AddImageURLs(flaggedUsers)
	w.bar.Increment(10)
	flaggedUsers = w.outfitFetcher.AddOutfits(flaggedUsers)
	w.bar.Increment(10)
	flaggedUsers = w.friendFetcher.AddFriends(flaggedUsers)
	w.bar.Increment(10)

	// Save all flagged users
	w.db.Users().SavePendingUsers(flaggedUsers)
	w.bar.Increment(10)

	w.logger.Info("Finished processing users",
		zap.Int("totalProcessed", len(userInfos)),
		zap.Int("flaggedUsers", len(flaggedUsers)),
		zap.Int("autoFlagged", len(flaggedUsers)-len(usersForAICheck)),
		zap.Int("aiFlagged", len(flaggedUsers)-(len(userInfos)-len(usersForAICheck))))
}

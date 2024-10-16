package user

import (
	"context"
	"time"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/openai/openai-go"
	"github.com/rotector/rotector/internal/common/database"
	"github.com/rotector/rotector/internal/common/fetcher"
	"go.uber.org/zap"
)

const (
	FriendUsersToProcess = 100
)

// FriendWorker represents a friend worker that processes user friends.
type FriendWorker struct {
	db               *database.Database
	roAPI            *api.API
	aiChecker        *fetcher.AIChecker
	userFetcher      *fetcher.UserFetcher
	outfitFetcher    *fetcher.OutfitFetcher
	thumbnailFetcher *fetcher.ThumbnailFetcher
	friendFetcher    *fetcher.FriendFetcher
	logger           *zap.Logger
}

// NewFriendWorker creates a new friend worker instance.
func NewFriendWorker(db *database.Database, openaiClient *openai.Client, roAPI *api.API, logger *zap.Logger) *FriendWorker {
	return &FriendWorker{
		db:               db,
		roAPI:            roAPI,
		aiChecker:        fetcher.NewAIChecker(openaiClient, logger),
		userFetcher:      fetcher.NewUserFetcher(roAPI, logger),
		outfitFetcher:    fetcher.NewOutfitFetcher(roAPI, logger),
		thumbnailFetcher: fetcher.NewThumbnailFetcher(roAPI, logger),
		friendFetcher:    fetcher.NewFriendFetcher(roAPI, logger),
		logger:           logger,
	}
}

// Start begins the friend worker's main loop.
func (w *FriendWorker) Start() {
	w.logger.Info("Friend Worker started")

	var remainingFriendIDs []uint64

	for {
		// Process friends in batches
		var err error
		remainingFriendIDs, err = w.processFriendsBatch(remainingFriendIDs)
		if err != nil {
			w.logger.Error("Error processing friends batch", zap.Error(err))
			time.Sleep(5 * time.Minute) // Wait before trying again
		}
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

	// Fetch user info for all collected friend IDs
	userInfos := w.userFetcher.FetchInfos(friendIDs[:FriendUsersToProcess])

	// Process the collected user infos
	w.processUsers(userInfos)

	return friendIDs[FriendUsersToProcess:], nil
}

// processUsers handles the processing of a batch of users.
func (w *FriendWorker) processUsers(userInfos []fetcher.Info) {
	w.logger.Info("Processing users", zap.Int("userInfos", len(userInfos)))

	var flaggedUsers []database.User
	var usersForAICheck []fetcher.Info

	for _, userInfo := range userInfos {
		// Get group IDs
		groupIDs := make([]uint64, len(userInfo.Groups))
		for i, group := range userInfo.Groups {
			groupIDs[i] = group.Group.ID
		}

		// Check if user belongs to any flagged groups
		flaggedGroupIDs, err := w.db.Groups().CheckFlaggedGroups(groupIDs)
		if err != nil {
			w.logger.Error("Error checking flagged groups", zap.Error(err), zap.Uint64("userID", userInfo.ID))
			continue
		}

		if len(flaggedGroupIDs) > 0 {
			// User belongs to flagged groups
			flaggedUsers = append(flaggedUsers, database.User{
				ID:            userInfo.ID,
				Name:          userInfo.Name,
				Description:   userInfo.Description,
				Reason:        "Member of flagged group(s)",
				Groups:        userInfo.Groups,
				FlaggedGroups: flaggedGroupIDs,
				Confidence:    float64(len(flaggedGroupIDs)) / float64(len(userInfo.Groups)),
			})

			w.logger.Info("User in flagged group",
				zap.Uint64("userID", userInfo.ID),
				zap.Uint64s("flaggedGroupIDs", flaggedGroupIDs))
		} else {
			// User not in flagged groups, add to AI check list
			usersForAICheck = append(usersForAICheck, userInfo)
		}
	}

	// Process remaining users with AI
	if len(usersForAICheck) > 0 {
		aiFlaggedUsers, err := w.aiChecker.CheckUsers(usersForAICheck)
		if err != nil {
			w.logger.Error("Error checking users with AI", zap.Error(err))
		} else {
			flaggedUsers = append(flaggedUsers, aiFlaggedUsers...)
		}
	}

	// Fetch necessary data for flagged users
	flaggedUsers = w.thumbnailFetcher.AddImageURLs(flaggedUsers)
	flaggedUsers = w.outfitFetcher.AddOutfits(flaggedUsers)
	flaggedUsers = w.friendFetcher.AddFriends(flaggedUsers)

	// Save all flagged users
	w.db.Users().SavePendingUsers(flaggedUsers)

	w.logger.Info("Finished processing users",
		zap.Int("totalProcessed", len(userInfos)),
		zap.Int("flaggedUsers", len(flaggedUsers)),
		zap.Int("groupFlagged", len(userInfos)-len(usersForAICheck)),
		zap.Int("aiFlagged", len(flaggedUsers)-(len(userInfos)-len(usersForAICheck))))
}

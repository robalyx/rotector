package user

import (
	"context"
	"time"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/jaxron/roapi.go/pkg/api/resources/groups"
	"github.com/openai/openai-go"
	"github.com/rotector/rotector/internal/common/database"
	"github.com/rotector/rotector/internal/common/fetcher"
	"go.uber.org/zap"
)

const (
	GroupUsersToProcess = 200
)

// GroupWorker represents a group worker that processes flagged groups.
type GroupWorker struct {
	db               *database.Database
	roAPI            *api.API
	aiChecker        *fetcher.AIChecker
	userFetcher      *fetcher.UserFetcher
	thumbnailFetcher *fetcher.ThumbnailFetcher
	outfitFetcher    *fetcher.OutfitFetcher
	friendFetcher    *fetcher.FriendFetcher
	logger           *zap.Logger
}

// NewGroupWorker creates a new group worker instance.
func NewGroupWorker(db *database.Database, openaiClient *openai.Client, roAPI *api.API, logger *zap.Logger) *GroupWorker {
	return &GroupWorker{
		db:               db,
		roAPI:            roAPI,
		aiChecker:        fetcher.NewAIChecker(openaiClient, logger),
		userFetcher:      fetcher.NewUserFetcher(roAPI, logger),
		thumbnailFetcher: fetcher.NewThumbnailFetcher(roAPI, logger),
		outfitFetcher:    fetcher.NewOutfitFetcher(roAPI, logger),
		friendFetcher:    fetcher.NewFriendFetcher(roAPI, logger),
		logger:           logger,
	}
}

// Start begins the group worker's main loop.
func (w *GroupWorker) Start() {
	w.logger.Info("Group Worker started")

	for {
		// Get the next flagged group to process and update its last_scanned time
		group, err := w.db.Groups().GetNextFlaggedGroup()
		if err != nil {
			w.logger.Error("Error getting next flagged group", zap.Error(err))
			time.Sleep(5 * time.Minute) // Wait before trying again
			continue
		}

		// Process the group
		w.processGroup(group.ID)
	}
}

// processGroup handles the processing of a single group.
func (w *GroupWorker) processGroup(groupID uint64) {
	cursor := ""
	var userInfos []fetcher.Info
	w.logger.Info("Processing group", zap.Uint64("groupID", groupID))

	for {
		// Fetch group users until we have at least GroupUsersToProcess or reach the end
		for len(userInfos) < GroupUsersToProcess {
			// Build the request for fetching group users
			builder := groups.NewGroupUsersBuilder(groupID).
				WithLimit(100).
				WithCursor(cursor)

			// Make the API call to fetch group users
			groupUsers, err := w.roAPI.Groups().GetGroupUsers(context.Background(), builder.Build())
			if err != nil {
				w.logger.Error("Error fetching group members", zap.Error(err))
				return
			}

			// Fetch user info for this batch
			userIDs := make([]uint64, len(groupUsers.Data))
			for i, groupUser := range groupUsers.Data {
				userIDs[i] = groupUser.User.UserID
			}
			batchUserInfos := w.userFetcher.FetchInfos(userIDs)
			userInfos = append(userInfos, batchUserInfos...)

			w.logger.Info("Fetched group users",
				zap.Uint64("groupID", groupID),
				zap.String("cursor", cursor),
				zap.Int("userInfos", len(userInfos)))

			// Stop if we've reached the end of the group
			if groupUsers.NextPageCursor == nil {
				cursor = ""
				break
			}
			cursor = *groupUsers.NextPageCursor
		}

		// Handle case where we have fewer than GroupUsersToProcess users
		usersToProcess := GroupUsersToProcess
		if len(userInfos) < GroupUsersToProcess {
			usersToProcess = len(userInfos)
		}

		w.processUsers(userInfos[:usersToProcess])

		// Remove the processed users
		userInfos = userInfos[usersToProcess:]

		// If we've reached the end of the group, exit the loop
		if cursor == "" {
			break
		}
	}

	w.logger.Info("Finished processing group", zap.Uint64("groupID", groupID))
}

// processUsers handles the processing of a batch of users.
func (w *GroupWorker) processUsers(userInfos []fetcher.Info) {
	w.logger.Info("Processing users", zap.Int("userInfos", len(userInfos)))

	// Process users with AI
	flaggedUsers, err := w.aiChecker.CheckUsers(userInfos)
	if err != nil {
		w.logger.Error("Error checking users with AI", zap.Error(err))
		return
	}

	// Fetch necessary data for flagged users
	flaggedUsers = w.thumbnailFetcher.AddImageURLs(flaggedUsers)
	flaggedUsers = w.outfitFetcher.AddOutfits(flaggedUsers)
	flaggedUsers = w.friendFetcher.AddFriends(flaggedUsers)

	// Save all flagged users
	w.db.Users().SavePendingUsers(flaggedUsers)

	w.logger.Info("Finished processing users",
		zap.Int("initialFlaggedUsers", len(flaggedUsers)),
		zap.Int("validatedFlaggedUsers", len(flaggedUsers)))
}

package user

import (
	"context"
	"time"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/jaxron/roapi.go/pkg/api/resources/groups"
	"github.com/openai/openai-go"
	"github.com/rotector/rotector/internal/common/database"
	"github.com/rotector/rotector/internal/common/fetcher"
	"github.com/rotector/rotector/internal/common/progress"
	"go.uber.org/zap"
)

const (
	GroupUsersToProcess = 200
)

// GroupWorker represents a group worker that processes flagged groups.
type GroupWorker struct {
	db               *database.Database
	roAPI            *api.API
	bar              *progress.Bar
	aiChecker        *fetcher.AIChecker
	userFetcher      *fetcher.UserFetcher
	thumbnailFetcher *fetcher.ThumbnailFetcher
	outfitFetcher    *fetcher.OutfitFetcher
	friendFetcher    *fetcher.FriendFetcher
	groupChecker     *fetcher.GroupChecker
	logger           *zap.Logger
}

// NewGroupWorker creates a new group worker instance.
func NewGroupWorker(db *database.Database, openaiClient *openai.Client, roAPI *api.API, bar *progress.Bar, logger *zap.Logger) *GroupWorker {
	return &GroupWorker{
		db:               db,
		roAPI:            roAPI,
		bar:              bar,
		aiChecker:        fetcher.NewAIChecker(openaiClient, logger),
		userFetcher:      fetcher.NewUserFetcher(roAPI, logger),
		thumbnailFetcher: fetcher.NewThumbnailFetcher(roAPI, logger),
		outfitFetcher:    fetcher.NewOutfitFetcher(roAPI, logger),
		friendFetcher:    fetcher.NewFriendFetcher(roAPI, logger),
		groupChecker:     fetcher.NewGroupChecker(db, logger),
		logger:           logger,
	}
}

// Start begins the group worker's main loop.
func (w *GroupWorker) Start() {
	w.logger.Info("Group Worker started")
	w.bar.SetTotal(100)

	var oldUserInfos []*fetcher.Info
	for {
		w.bar.Reset()

		// Step 1: Get next flagged group (20%)
		group, err := w.db.Groups().GetNextFlaggedGroup()
		if err != nil {
			w.logger.Error("Error getting next flagged group", zap.Error(err))
			time.Sleep(5 * time.Minute) // Wait before trying again
			continue
		}
		w.bar.Increment(20)

		// Step 2: Get group users (20%)
		userInfos, err := w.processGroup(group.ID, oldUserInfos)
		if err != nil {
			w.logger.Error("Error processing group", zap.Error(err), zap.Uint64("groupID", group.ID))
			time.Sleep(5 * time.Minute) // Wait before trying again
			continue
		}
		w.bar.Increment(20)

		// Step 3: Process users (60%)
		w.processUsers(userInfos[:GroupUsersToProcess])

		// Step 4: Prepare for next batch
		oldUserInfos = userInfos[GroupUsersToProcess:]

		// Short pause before next iteration
		time.Sleep(1 * time.Second)
	}
}

// processGroup handles the processing of a single group.
func (w *GroupWorker) processGroup(groupID uint64, userInfos []*fetcher.Info) ([]*fetcher.Info, error) {
	w.logger.Info("Processing group", zap.Uint64("groupID", groupID))

	// Step 2: Fetch group users
	cursor := ""
	for len(userInfos) < GroupUsersToProcess {
		builder := groups.NewGroupUsersBuilder(groupID).
			WithLimit(100).
			WithCursor(cursor)

		groupUsers, err := w.roAPI.Groups().GetGroupUsers(context.Background(), builder.Build())
		if err != nil {
			w.logger.Error("Error fetching group members", zap.Error(err))
			return nil, err
		}

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

		if groupUsers.NextPageCursor == nil {
			break
		}
		cursor = *groupUsers.NextPageCursor
	}

	return userInfos, nil
}

// processUsers handles the processing of a batch of users.
func (w *GroupWorker) processUsers(userInfos []*fetcher.Info) {
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
		zap.Int("initialFlaggedUsers", len(flaggedUsers)),
		zap.Int("validatedFlaggedUsers", len(flaggedUsers)))
}

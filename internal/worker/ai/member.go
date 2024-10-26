package ai

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

// MemberWorker represents a group worker that processes group members.
type MemberWorker struct {
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

// NewMemberWorker creates a new group worker instance.
func NewMemberWorker(db *database.Database, openaiClient *openai.Client, roAPI *api.API, bar *progress.Bar, logger *zap.Logger) *MemberWorker {
	return &MemberWorker{
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
func (g *MemberWorker) Start() {
	g.logger.Info("Group Worker started")
	g.bar.SetTotal(100)

	var oldUserIDs []uint64
	for {
		g.bar.Reset()

		// Step 1: Get next confirmed group (10%)
		g.bar.SetStepMessage("Fetching next confirmed group")
		group, err := g.db.Groups().GetNextConfirmedGroup()
		if err != nil {
			g.logger.Error("Error getting next confirmed group", zap.Error(err))
			time.Sleep(5 * time.Minute) // Wait before trying again
			continue
		}
		g.bar.Increment(10)

		// Step 2: Get group users (15%)
		g.bar.SetStepMessage("Processing group users")
		userIDs, err := g.processGroup(group.ID, oldUserIDs)
		if err != nil {
			g.logger.Error("Error processing group", zap.Error(err), zap.Uint64("groupID", group.ID))
			time.Sleep(5 * time.Minute) // Wait before trying again
			continue
		}
		g.bar.Increment(15)

		// Step 3: Fetch user info (15%)
		g.bar.SetStepMessage("Fetching user info")
		userInfos := g.userFetcher.FetchInfos(userIDs[:GroupUsersToProcess])
		g.bar.Increment(15)

		// Step 4: Process users (60%)
		g.processUsers(userInfos)

		// Step 5: Prepare for next batch
		oldUserIDs = userIDs[GroupUsersToProcess:]

		// Short pause before next iteration
		time.Sleep(1 * time.Second)
	}
}

// processGroup handles the processing of a single group.
func (g *MemberWorker) processGroup(groupID uint64, userIDs []uint64) ([]uint64, error) {
	g.logger.Info("Processing group", zap.Uint64("groupID", groupID))

	cursor := ""
	for len(userIDs) < GroupUsersToProcess {
		builder := groups.NewGroupUsersBuilder(groupID).
			WithLimit(100).
			WithCursor(cursor)

		groupUsers, err := g.roAPI.Groups().GetGroupUsers(context.Background(), builder.Build())
		if err != nil {
			g.logger.Error("Error fetching group members", zap.Error(err))
			return nil, err
		}

		// Extract user IDs
		newUserIDs := make([]uint64, len(groupUsers.Data))
		for i, groupUser := range groupUsers.Data {
			newUserIDs[i] = groupUser.User.UserID
		}

		// Check which users already exist in the database
		existingUsers, err := g.db.Users().CheckExistingUsers(newUserIDs)
		if err != nil {
			g.logger.Error("Error checking existing users", zap.Error(err))
			continue
		}

		// Add only new users to the userIDs slice
		for _, userID := range newUserIDs {
			if _, exists := existingUsers[userID]; !exists {
				userIDs = append(userIDs, userID)
			}
		}

		g.logger.Info("Fetched group users",
			zap.Uint64("groupID", groupID),
			zap.String("cursor", cursor),
			zap.Int("totalUsers", len(groupUsers.Data)),
			zap.Int("newUsers", len(newUserIDs)-len(existingUsers)),
			zap.Int("userIDs", len(userIDs)))

		if groupUsers.NextPageCursor == nil {
			break
		}
		cursor = *groupUsers.NextPageCursor
	}

	return userIDs, nil
}

// processUsers handles the processing of a batch of users.
func (g *MemberWorker) processUsers(userInfos []*fetcher.Info) {
	g.logger.Info("Processing users", zap.Int("userInfos", len(userInfos)))

	var flaggedUsers []*database.User
	var usersForAICheck []*fetcher.Info

	// Check if users belong to a certain number of flagged groups
	g.bar.SetStepMessage("Checking user groups")
	for _, userInfo := range userInfos {
		user, autoFlagged, err := g.groupChecker.CheckUserGroups(userInfo)
		if err != nil {
			g.logger.Error("Error checking user groups", zap.Error(err), zap.Uint64("userID", userInfo.ID))
			continue
		}

		if autoFlagged {
			flaggedUsers = append(flaggedUsers, user)
		} else {
			usersForAICheck = append(usersForAICheck, userInfo)
		}
	}
	g.bar.Increment(10)

	// Process remaining users with AI
	g.bar.SetStepMessage("Checking users with AI")
	if len(usersForAICheck) > 0 {
		aiFlaggedUsers, err := g.aiChecker.CheckUsers(usersForAICheck)
		if err != nil {
			g.logger.Error("Error checking users with AI", zap.Error(err))
		} else {
			flaggedUsers = append(flaggedUsers, aiFlaggedUsers...)
		}
	}
	g.bar.Increment(10)

	// Fetch necessary data for flagged users
	g.bar.SetStepMessage("Adding image URLs")
	flaggedUsers = g.thumbnailFetcher.AddImageURLs(flaggedUsers)
	g.bar.Increment(10)

	g.bar.SetStepMessage("Adding outfits")
	flaggedUsers = g.outfitFetcher.AddOutfits(flaggedUsers)
	g.bar.Increment(10)

	g.bar.SetStepMessage("Adding friends")
	flaggedUsers = g.friendFetcher.AddFriends(flaggedUsers)
	g.bar.Increment(10)

	// Save all flagged users
	g.bar.SetStepMessage("Saving flagged users")
	g.db.Users().SaveFlaggedUsers(flaggedUsers)
	g.bar.Increment(10)

	g.logger.Info("Finished processing users",
		zap.Int("totalProcessed", len(userInfos)),
		zap.Int("flaggedUsers", len(flaggedUsers)))
}

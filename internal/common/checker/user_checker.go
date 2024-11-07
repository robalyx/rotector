package checker

import (
	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/openai/openai-go"
	"github.com/rotector/rotector/internal/common/database"
	"github.com/rotector/rotector/internal/common/fetcher"
	"github.com/rotector/rotector/internal/common/progress"
	"go.uber.org/zap"
)

// UserChecker coordinates the checking process by combining results from
// multiple checking methods (AI, groups, friends) and managing the progress bar.
type UserChecker struct {
	db               *database.Database
	bar              *progress.Bar
	userFetcher      *fetcher.UserFetcher
	outfitFetcher    *fetcher.OutfitFetcher
	thumbnailFetcher *fetcher.ThumbnailFetcher
	aiChecker        *AIChecker
	groupChecker     *GroupChecker
	friendChecker    *FriendChecker
	logger           *zap.Logger
}

// NewUserChecker creates a UserChecker with all required dependencies.
func NewUserChecker(
	db *database.Database,
	bar *progress.Bar,
	roAPI *api.API,
	openaiClient *openai.Client,
	userFetcher *fetcher.UserFetcher,
	logger *zap.Logger,
) *UserChecker {
	return &UserChecker{
		db:               db,
		bar:              bar,
		userFetcher:      userFetcher,
		outfitFetcher:    fetcher.NewOutfitFetcher(roAPI, logger),
		thumbnailFetcher: fetcher.NewThumbnailFetcher(roAPI, logger),
		aiChecker:        NewAIChecker(openaiClient, logger),
		groupChecker:     NewGroupChecker(db, logger),
		friendChecker:    NewFriendChecker(db, logger),
		logger:           logger,
	}
}

// ProcessUsers runs users through multiple checking stages:
// 1. Group checking - flags users in multiple flagged groups
// 2. Friend checking - flags users with many flagged friends
// 3. AI checking - analyzes user content for violations
// After flagging, it loads additional data (outfits, thumbnails) for flagged users.
// Returns IDs of users that failed AI validation for retry.
func (c *UserChecker) ProcessUsers(userInfos []*fetcher.Info) []uint64 {
	c.logger.Info("Processing users", zap.Int("userInfos", len(userInfos)))

	var flaggedUsers []*database.User
	var usersForAICheck []*fetcher.Info
	var failedValidationIDs []uint64

	// Check if users belong to flagged groups
	c.bar.SetStepMessage("Checking user groups")
	flaggedUsersFromGroups, remainingUsers := c.groupChecker.ProcessUsers(userInfos)
	flaggedUsers = append(flaggedUsers, flaggedUsersFromGroups...)
	usersForAICheck = remainingUsers
	c.bar.Increment(10)

	// Check users based on their friends
	c.bar.SetStepMessage("Checking user friends")
	flaggedUsersFromFriends, remainingUsers := c.friendChecker.ProcessUsers(usersForAICheck)
	flaggedUsers = append(flaggedUsers, flaggedUsersFromFriends...)
	usersForAICheck = remainingUsers
	c.bar.Increment(10)

	// Process remaining users with AI
	c.bar.SetStepMessage("Checking users with AI")
	if len(usersForAICheck) > 0 {
		aiFlaggedUsers, failedIDs, err := c.aiChecker.ProcessUsers(usersForAICheck)
		if err != nil {
			c.logger.Error("Error checking users with AI", zap.Error(err))
		} else {
			flaggedUsers = append(flaggedUsers, aiFlaggedUsers...)
			failedValidationIDs = append(failedValidationIDs, failedIDs...)
		}
	}
	c.bar.Increment(10)

	// Stop if no users were flagged
	if len(flaggedUsers) == 0 {
		c.logger.Info("No flagged users found", zap.Int("userInfos", len(userInfos)))
		c.bar.Increment(30)
		return failedValidationIDs
	}

	// Load additional data for flagged users
	c.bar.SetStepMessage("Adding image URLs")
	flaggedUsers = c.thumbnailFetcher.AddImageURLs(flaggedUsers)
	c.bar.Increment(10)

	c.bar.SetStepMessage("Adding outfits")
	flaggedUsers = c.outfitFetcher.AddOutfits(flaggedUsers)
	c.bar.Increment(10)

	// Save flagged users to database
	c.bar.SetStepMessage("Saving flagged users")
	c.db.Users().SaveFlaggedUsers(flaggedUsers)
	c.bar.Increment(10)

	c.logger.Info("Finished processing users",
		zap.Int("totalProcessed", len(userInfos)),
		zap.Int("flaggedUsers", len(flaggedUsers)))

	return failedValidationIDs
}

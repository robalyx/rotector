package checker

import (
	"context"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/openai/openai-go"
	"github.com/rotector/rotector/internal/common/database"
	"github.com/rotector/rotector/internal/common/fetcher"
	"github.com/rotector/rotector/internal/common/progress"
	"github.com/rotector/rotector/internal/common/translator"
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
	followerChecker  *FollowerChecker
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
	translator := translator.New(roAPI.GetClient())
	aiChecker := NewAIChecker(openaiClient, translator, logger)
	return &UserChecker{
		db:               db,
		bar:              bar,
		userFetcher:      userFetcher,
		outfitFetcher:    fetcher.NewOutfitFetcher(roAPI, logger),
		thumbnailFetcher: fetcher.NewThumbnailFetcher(roAPI, logger),
		aiChecker:        aiChecker,
		groupChecker:     NewGroupChecker(db, logger),
		friendChecker:    NewFriendChecker(db, aiChecker, logger),
		followerChecker:  NewFollowerChecker(roAPI, logger),
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

	// Check if users belong to flagged groups (20%)
	c.bar.SetStepMessage("Checking user groups", 20)
	flaggedUsersFromGroups, remainingUsers := c.groupChecker.ProcessUsers(userInfos)
	flaggedUsers = append(flaggedUsers, flaggedUsersFromGroups...)
	usersForAICheck = remainingUsers

	// Check users based on their friends (40%)
	c.bar.SetStepMessage("Checking user friends", 40)
	flaggedUsersFromFriends, remainingUsers := c.friendChecker.ProcessUsers(usersForAICheck)
	flaggedUsers = append(flaggedUsers, flaggedUsersFromFriends...)
	usersForAICheck = remainingUsers

	// Process remaining users with AI (60%)
	c.bar.SetStepMessage("Checking users with AI", 60)
	if len(usersForAICheck) > 0 {
		aiFlaggedUsers, failedIDs, err := c.aiChecker.ProcessUsers(usersForAICheck)
		if err != nil {
			c.logger.Error("Error checking users with AI", zap.Error(err))
		} else {
			flaggedUsers = append(flaggedUsers, aiFlaggedUsers...)
			failedValidationIDs = append(failedValidationIDs, failedIDs...)
		}
	}

	// Stop if no users were flagged
	if len(flaggedUsers) == 0 {
		c.logger.Info("No flagged users found", zap.Int("userInfos", len(userInfos)))
		return failedValidationIDs
	}

	// Check followers for flagged users (70%)
	c.bar.SetStepMessage("Checking user followers", 70)
	flaggedUsers = c.followerChecker.ProcessUsers(flaggedUsers)

	// Load additional data for flagged users (80%)
	c.bar.SetStepMessage("Adding image URLs", 80)
	flaggedUsers = c.thumbnailFetcher.AddImageURLs(flaggedUsers)

	c.bar.SetStepMessage("Adding outfits", 90)
	flaggedUsers = c.outfitFetcher.AddOutfits(flaggedUsers)

	// Save flagged users to database (100%)
	c.bar.SetStepMessage("Saving flagged users", 100)
	c.db.Users().SaveFlaggedUsers(context.Background(), flaggedUsers)

	c.logger.Info("Finished processing users",
		zap.Int("totalProcessed", len(userInfos)),
		zap.Int("flaggedUsers", len(flaggedUsers)))

	return failedValidationIDs
}

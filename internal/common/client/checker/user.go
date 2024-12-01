package checker

import (
	"context"
	"fmt"

	"github.com/rotector/rotector/internal/common/client/fetcher"
	"github.com/rotector/rotector/internal/common/progress"
	"github.com/rotector/rotector/internal/common/setup"
	"github.com/rotector/rotector/internal/common/storage/database"
	"github.com/rotector/rotector/internal/common/storage/database/models"
	"github.com/rotector/rotector/internal/common/translator"
	"go.uber.org/zap"
)

// UserChecker coordinates the checking process by combining results from
// multiple checking methods (AI, groups, friends) and managing the progress bar.
type UserChecker struct {
	db               *database.Client
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
func NewUserChecker(app *setup.App, bar *progress.Bar, userFetcher *fetcher.UserFetcher, logger *zap.Logger) *UserChecker {
	translator := translator.New(app.RoAPI.GetClient())
	aiChecker := NewAIChecker(app, translator, logger)

	return &UserChecker{
		db:               app.DB,
		bar:              bar,
		userFetcher:      userFetcher,
		outfitFetcher:    fetcher.NewOutfitFetcher(app.RoAPI, logger),
		thumbnailFetcher: fetcher.NewThumbnailFetcher(app.RoAPI, logger),
		aiChecker:        aiChecker,
		groupChecker:     NewGroupChecker(app.DB, logger),
		friendChecker:    NewFriendChecker(app.DB, aiChecker, logger),
		followerChecker:  NewFollowerChecker(app.RoAPI, logger),
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

	var flaggedUsers []*models.User
	var failedValidationIDs []uint64

	// Check if users belong to flagged groups (20%)
	c.bar.SetStepMessage("Checking user groups", 20)
	flaggedUsersFromGroups, remainingUsers := c.groupChecker.ProcessUsers(userInfos)
	flaggedUsers = append(flaggedUsers, flaggedUsersFromGroups...)

	// Check users based on their friends (40%)
	c.bar.SetStepMessage("Checking user friends", 40)
	flaggedUsersFromFriends := c.friendChecker.ProcessUsers(remainingUsers)
	flaggedUsers = append(flaggedUsers, flaggedUsersFromFriends...)

	// Process all users with AI (60%)
	c.bar.SetStepMessage("Checking users with AI", 60)
	aiFlaggedUsers, failedIDs, err := c.aiChecker.ProcessUsers(userInfos)
	if err != nil {
		c.logger.Error("Error checking users with AI", zap.Error(err))
	} else {
		// Create a map of existing flagged users by ID for easy lookup
		flaggedByID := make(map[uint64]*models.User)
		for _, user := range flaggedUsers {
			flaggedByID[user.ID] = user
		}

		// Process AI flagged users
		for _, aiUser := range aiFlaggedUsers {
			if existingUser, ok := flaggedByID[aiUser.ID]; ok {
				// User was flagged by both friends and AI
				existingUser.Confidence = 1.0
				existingUser.Reason = fmt.Sprintf("%s\n\nAI Analysis: %s", existingUser.Reason, aiUser.Reason)
				existingUser.FlaggedContent = aiUser.FlaggedContent
			} else {
				// User was only flagged by AI
				flaggedUsers = append(flaggedUsers, aiUser)
			}
		}

		failedValidationIDs = append(failedValidationIDs, failedIDs...)
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

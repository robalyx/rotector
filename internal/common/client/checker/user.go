package checker

import (
	"context"
	"fmt"

	"github.com/rotector/rotector/internal/common/client/fetcher"
	"github.com/rotector/rotector/internal/common/setup"
	"github.com/rotector/rotector/internal/common/storage/database"
	"github.com/rotector/rotector/internal/common/translator"
	"go.uber.org/zap"
)

// UserChecker coordinates the checking process by combining results from
// multiple checking methods (AI, groups, friends) and managing the progress bar.
type UserChecker struct {
	app           *setup.App
	db            *database.Client
	userFetcher   *fetcher.UserFetcher
	aiChecker     *AIChecker
	groupChecker  *GroupChecker
	friendChecker *FriendChecker
	logger        *zap.Logger
}

// NewUserChecker creates a UserChecker with all required dependencies.
func NewUserChecker(app *setup.App, userFetcher *fetcher.UserFetcher, logger *zap.Logger) *UserChecker {
	translator := translator.New(app.RoAPI.GetClient())
	aiChecker := NewAIChecker(app, translator, logger)

	return &UserChecker{
		app:           app,
		db:            app.DB,
		userFetcher:   userFetcher,
		aiChecker:     aiChecker,
		groupChecker:  NewGroupChecker(app.DB, logger),
		friendChecker: NewFriendChecker(app, logger),
		logger:        logger,
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

	// Process group checker results
	flaggedUsers := c.groupChecker.ProcessUsers(userInfos)

	// Process friend checker results
	flaggedByFriends := c.friendChecker.ProcessUsers(userInfos)
	for userID, friendUser := range flaggedByFriends {
		if existingUser, ok := flaggedUsers[userID]; ok {
			// Combine reasons and update confidence
			existingUser.Reason = fmt.Sprintf("%s\n\n%s", existingUser.Reason, friendUser.Reason)
			existingUser.Confidence = 1.0
		} else {
			flaggedUsers[userID] = friendUser
		}
	}

	// Process AI results
	flaggedByAI, failedIDs, err := c.aiChecker.ProcessUsers(userInfos)
	if err != nil {
		c.logger.Error("Error checking users with AI", zap.Error(err))
	} else {
		for userID, aiUser := range flaggedByAI {
			if existingUser, ok := flaggedUsers[userID]; ok {
				// Combine reasons and update confidence
				existingUser.Reason = fmt.Sprintf("%s\n\n%s", existingUser.Reason, aiUser.Reason)
				existingUser.Confidence = 1.0
				existingUser.FlaggedContent = aiUser.FlaggedContent
			} else {
				flaggedUsers[userID] = aiUser
			}
		}
	}

	// Stop if no users were flagged
	if len(flaggedUsers) == 0 {
		c.logger.Info("No flagged users found", zap.Int("userInfos", len(userInfos)))
		return failedIDs
	}

	// Fetch additional user data concurrently
	flaggedUsers = c.userFetcher.FetchAdditionalUserData(flaggedUsers)

	// Check if any flagged users have a follower count above the threshold
	for _, user := range flaggedUsers {
		if user.FollowerCount >= c.app.Config.Worker.ThresholdLimits.MinFollowersForPopular {
			user.Reason = "⚠️ **WARNING: Popular user with large amount of followers**\n\n" + user.Reason
			user.Confidence = 1.0

			c.logger.Info("Popular user flagged",
				zap.Uint64("userID", user.ID),
				zap.String("username", user.Name),
				zap.Uint64("followers", user.FollowerCount))
		}
	}

	// Save flagged users to database
	if err := c.db.Users().SaveFlaggedUsers(context.Background(), flaggedUsers); err != nil {
		c.logger.Error("Failed to save flagged users", zap.Error(err))
	}

	c.logger.Info("Finished processing users",
		zap.Int("totalProcessed", len(userInfos)),
		zap.Int("flaggedUsers", len(flaggedUsers)))

	return failedIDs
}

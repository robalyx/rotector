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
	db               *database.Client
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
func NewUserChecker(app *setup.App, userFetcher *fetcher.UserFetcher, logger *zap.Logger) *UserChecker {
	translator := translator.New(app.RoAPI.GetClient())
	aiChecker := NewAIChecker(app, translator, logger)

	return &UserChecker{
		db:               app.DB,
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

	// Process group checker results
	flaggedUsers, remainingUsers := c.groupChecker.ProcessUsers(userInfos)

	// Process friend checker results
	flaggedByFriends := c.friendChecker.ProcessUsers(remainingUsers)
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

	// Check followers for flagged users
	flaggedUsers = c.followerChecker.ProcessUsers(flaggedUsers)

	// Load additional data for flagged users
	flaggedUsers = c.thumbnailFetcher.AddImageURLs(flaggedUsers)
	flaggedUsers = c.outfitFetcher.AddOutfits(flaggedUsers)

	// Save flagged users to database
	c.db.Users().SaveFlaggedUsers(context.Background(), flaggedUsers)

	c.logger.Info("Finished processing users",
		zap.Int("totalProcessed", len(userInfos)),
		zap.Int("flaggedUsers", len(flaggedUsers)))

	return failedIDs
}

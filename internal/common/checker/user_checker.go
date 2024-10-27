package checker

import (
	"fmt"
	"math"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/openai/openai-go"
	"github.com/rotector/rotector/internal/common/database"
	"github.com/rotector/rotector/internal/common/fetcher"
	"github.com/rotector/rotector/internal/common/progress"
	"go.uber.org/zap"
)

// UserChecker handles the common user checking logic for workers.
type UserChecker struct {
	db               *database.Database
	bar              *progress.Bar
	userFetcher      *fetcher.UserFetcher
	outfitFetcher    *fetcher.OutfitFetcher
	thumbnailFetcher *fetcher.ThumbnailFetcher
	aiChecker        *AIChecker
	groupChecker     *GroupChecker
	logger           *zap.Logger
}

// NewUserChecker creates a new UserChecker instance.
func NewUserChecker(db *database.Database, bar *progress.Bar, roAPI *api.API, openaiClient *openai.Client, userFetcher *fetcher.UserFetcher, logger *zap.Logger) *UserChecker {
	return &UserChecker{
		db:               db,
		bar:              bar,
		userFetcher:      userFetcher,
		outfitFetcher:    fetcher.NewOutfitFetcher(roAPI, logger),
		thumbnailFetcher: fetcher.NewThumbnailFetcher(roAPI, logger),
		aiChecker:        NewAIChecker(openaiClient, logger),
		groupChecker:     NewGroupChecker(db, logger),
		logger:           logger,
	}
}

// ProcessUsers handles the processing of a batch of users.
func (c *UserChecker) ProcessUsers(userInfos []*fetcher.Info) {
	c.logger.Info("Processing users", zap.Int("userInfos", len(userInfos)))

	var flaggedUsers []*database.User
	var usersForAICheck []*fetcher.Info

	// Check if users belong to a certain number of flagged groups
	c.bar.SetStepMessage("Checking user groups")
	for _, userInfo := range userInfos {
		user, autoFlagged, err := c.groupChecker.ProcessUserGroups(userInfo)
		if err != nil {
			c.logger.Error("Error checking user groups", zap.Error(err), zap.Uint64("userID", userInfo.ID))
			continue
		}

		if autoFlagged {
			flaggedUsers = append(flaggedUsers, user)
		} else {
			usersForAICheck = append(usersForAICheck, userInfo)
		}
	}
	c.bar.Increment(10)

	// Check users based on their friends
	c.bar.SetStepMessage("Checking user friends")
	flaggedUsersFromFriends, remainingUsers := c.checkUserFriends(usersForAICheck)
	flaggedUsers = append(flaggedUsers, flaggedUsersFromFriends...)
	usersForAICheck = remainingUsers
	c.bar.Increment(10)

	// Process remaining users with AI
	c.bar.SetStepMessage("Checking users with AI")
	if len(usersForAICheck) > 0 {
		aiFlaggedUsers, err := c.aiChecker.ProcessUsers(usersForAICheck)
		if err != nil {
			c.logger.Error("Error checking users with AI", zap.Error(err))
		} else {
			flaggedUsers = append(flaggedUsers, aiFlaggedUsers...)
		}
	}
	c.bar.Increment(10)

	// Fetch necessary data for flagged users
	c.bar.SetStepMessage("Adding image URLs")
	flaggedUsers = c.thumbnailFetcher.AddImageURLs(flaggedUsers)
	c.bar.Increment(10)

	c.bar.SetStepMessage("Adding outfits")
	flaggedUsers = c.outfitFetcher.AddOutfits(flaggedUsers)
	c.bar.Increment(10)

	// Save all flagged users
	c.bar.SetStepMessage("Saving flagged users")
	c.db.Users().SaveFlaggedUsers(flaggedUsers)
	c.bar.Increment(10)

	c.logger.Info("Finished processing users",
		zap.Int("totalProcessed", len(userInfos)),
		zap.Int("flaggedUsers", len(flaggedUsers)))
}

// checkUserFriends checks if a user should be flagged based on their friends.
func (c *UserChecker) checkUserFriends(userInfos []*fetcher.Info) ([]*database.User, []*fetcher.Info) {
	var flaggedUsers []*database.User
	var remainingUsers []*fetcher.Info

	for _, userInfo := range userInfos {
		// If the user has no friends, skip them
		if len(userInfo.Friends) == 0 {
			remainingUsers = append(remainingUsers, userInfo)
			continue
		}

		// Extract friend IDs
		friendIDs := make([]uint64, len(userInfo.Friends))
		for i, friend := range userInfo.Friends {
			friendIDs[i] = friend.ID
		}

		// Check if friends already exist in the database
		existingUsers, err := c.db.Users().CheckExistingUsers(friendIDs)
		if err != nil {
			c.logger.Error("Error checking existing users", zap.Error(err), zap.Uint64("userID", userInfo.ID))
			remainingUsers = append(remainingUsers, userInfo)
			continue
		}

		// If the user has 8 or more flagged friends, or 50% or more of their friends are flagged, flag the user
		flaggedCount := len(existingUsers)
		flaggedRatio := float64(flaggedCount) / float64(len(userInfo.Friends))
		if flaggedCount >= 8 || flaggedRatio >= 0.5 {
			flaggedUser := &database.User{
				ID:          userInfo.ID,
				Name:        userInfo.Name,
				DisplayName: userInfo.DisplayName,
				Description: userInfo.Description,
				CreatedAt:   userInfo.CreatedAt,
				Reason:      fmt.Sprintf("User has %d flagged friends (%.2f%%)", flaggedCount, flaggedRatio*100),
				Groups:      userInfo.Groups,
				Friends:     userInfo.Friends,
				Confidence:  math.Round(flaggedRatio*100) / 100, // Round to 2 decimal places
				LastUpdated: userInfo.LastUpdated,
			}
			flaggedUsers = append(flaggedUsers, flaggedUser)
		} else {
			remainingUsers = append(remainingUsers, userInfo)
		}
	}

	return flaggedUsers, remainingUsers
}

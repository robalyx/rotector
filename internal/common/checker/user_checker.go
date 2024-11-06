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

// checkUserFriends checks if a user should be flagged based on their friends.
func (c *UserChecker) checkUserFriends(userInfos []*fetcher.Info) ([]*database.User, []*fetcher.Info) {
	var flaggedUsers []*database.User
	var remainingUsers []*fetcher.Info

	for _, userInfo := range userInfos {
		if len(userInfo.Friends) < 3 {
			remainingUsers = append(remainingUsers, userInfo)
			continue
		}

		// Extract friend IDs
		friendIDs := make([]uint64, len(userInfo.Friends))
		for i, friend := range userInfo.Friends {
			friendIDs[i] = friend.ID
		}

		// Check which users already exist in the database
		existingUsers, err := c.db.Users().CheckExistingUsers(friendIDs)
		if err != nil {
			c.logger.Error("Error checking existing users", zap.Error(err), zap.Uint64("userID", userInfo.ID))
			remainingUsers = append(remainingUsers, userInfo)
			continue
		}

		// Count flagged friends
		flaggedCount := 0
		for _, status := range existingUsers {
			if status == database.UserTypeConfirmed || status == database.UserTypeFlagged {
				flaggedCount++
			}
		}

		// If the user has 8 or more flagged friends, or 50% or more of their friends are flagged, flag the user
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

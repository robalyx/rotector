package checker

import (
	"context"
	"fmt"

	"github.com/robalyx/rotector/internal/common/client/ai"
	"github.com/robalyx/rotector/internal/common/client/fetcher"
	"github.com/robalyx/rotector/internal/common/setup"
	"github.com/robalyx/rotector/internal/common/storage/database"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/robalyx/rotector/internal/common/translator"
	"go.uber.org/zap"
)

// UserChecker coordinates the checking process by combining results from
// multiple checking methods (AI, groups, friends) and managing the progress bar.
type UserChecker struct {
	app           *setup.App
	db            *database.Client
	userFetcher   *fetcher.UserFetcher
	userAnalyzer  *ai.UserAnalyzer
	groupChecker  *GroupChecker
	friendChecker *FriendChecker
	logger        *zap.Logger
}

// NewUserChecker creates a UserChecker with all required dependencies.
func NewUserChecker(app *setup.App, userFetcher *fetcher.UserFetcher, logger *zap.Logger) *UserChecker {
	translator := translator.New(app.RoAPI.GetClient())
	userAnalyzer := ai.NewUserAnalyzer(app, translator, logger)

	return &UserChecker{
		app:          app,
		db:           app.DB,
		userFetcher:  userFetcher,
		userAnalyzer: userAnalyzer,
		groupChecker: NewGroupChecker(app.DB, logger,
			app.Config.Worker.ThresholdLimits.MaxGroupMembersTrack,
			app.Config.Worker.ThresholdLimits.MinFlaggedOverride,
			app.Config.Worker.ThresholdLimits.MinFlaggedPercentage,
		),
		friendChecker: NewFriendChecker(app, logger),
		logger:        logger,
	}
}

// ProcessUsers runs users through multiple checking stage.
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
	flaggedByAI, failedIDs, err := c.userAnalyzer.ProcessUsers(userInfos)
	if err == nil {
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
	if err := c.db.Users().SaveUsers(context.Background(), flaggedUsers); err != nil {
		c.logger.Error("Failed to save users", zap.Error(err))
	}

	// Track flagged users' group memberships
	go c.trackFlaggedUsersGroups(flaggedUsers)

	c.logger.Info("Finished processing users",
		zap.Int("totalProcessed", len(userInfos)),
		zap.Int("flaggedUsers", len(flaggedUsers)))

	return failedIDs
}

// trackFlaggedUsersGroups adds flagged users' group memberships to tracking.
func (c *UserChecker) trackFlaggedUsersGroups(flaggedUsers map[uint64]*types.User) {
	groupUsersTracking := make(map[uint64][]uint64)

	// Collect group memberships for flagged users
	for userID, user := range flaggedUsers {
		for _, group := range user.Groups {
			// Only track if member count is below threshold
			if group.Group.MemberCount <= c.app.Config.Worker.ThresholdLimits.MaxGroupMembersTrack {
				groupUsersTracking[group.Group.ID] = append(groupUsersTracking[group.Group.ID], userID)
			}
		}
	}

	// Add to tracking if we have any data
	if len(groupUsersTracking) > 0 {
		if err := c.db.Tracking().AddUsersToGroupsTracking(context.Background(), groupUsersTracking); err != nil {
			c.logger.Error("Failed to add flagged users to groups tracking", zap.Error(err))
		}
	}
}

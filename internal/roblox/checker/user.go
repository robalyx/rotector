package checker

import (
	"context"

	"github.com/robalyx/rotector/internal/ai"
	"github.com/robalyx/rotector/internal/database"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/database/types/enum"
	"github.com/robalyx/rotector/internal/roblox/fetcher"
	"github.com/robalyx/rotector/internal/setup"
	"github.com/robalyx/rotector/internal/translator"
	"github.com/robalyx/rotector/pkg/utils"
	"go.uber.org/zap"
)

// UserChecker coordinates the checking process by combining results from
// multiple checking methods (AI, groups, friends) and managing the progress bar.
type UserChecker struct {
	app            *setup.App
	db             database.Client
	userFetcher    *fetcher.UserFetcher
	userAnalyzer   *ai.UserAnalyzer
	outfitAnalyzer *ai.OutfitAnalyzer
	groupChecker   *GroupChecker
	friendChecker  *FriendChecker
	condoChecker   *CondoChecker
	logger         *zap.Logger
}

// NewUserChecker creates a UserChecker with all required dependencies.
func NewUserChecker(app *setup.App, userFetcher *fetcher.UserFetcher, logger *zap.Logger) *UserChecker {
	translator := translator.New(app.RoAPI.GetClient())
	userAnalyzer := ai.NewUserAnalyzer(app, translator, logger)
	outfitAnalyzer := ai.NewOutfitAnalyzer(app, logger)

	return &UserChecker{
		app:            app,
		db:             app.DB,
		userFetcher:    userFetcher,
		userAnalyzer:   userAnalyzer,
		outfitAnalyzer: outfitAnalyzer,
		groupChecker: NewGroupChecker(app.DB, logger,
			app.Config.Worker.ThresholdLimits.MaxGroupMembersTrack,
			app.Config.Worker.ThresholdLimits.MinFlaggedOverride,
			app.Config.Worker.ThresholdLimits.MinFlaggedPercentage,
		),
		friendChecker: NewFriendChecker(app, logger),
		condoChecker:  NewCondoChecker(app.DB, logger),
		logger:        logger.Named("user_checker"),
	}
}

// ProcessUsers runs users through multiple checking stage.
func (c *UserChecker) ProcessUsers(userInfos []*types.User) {
	c.logger.Info("Processing users", zap.Int("userInfos", len(userInfos)))

	// Initialize map to store reasons
	reasonsMap := make(map[uint64]types.Reasons[enum.UserReasonType])

	// Process group checker
	c.groupChecker.ProcessUsers(userInfos, reasonsMap)

	// Process friend checker
	c.friendChecker.ProcessUsers(userInfos, reasonsMap)

	// Process user analysis
	c.userAnalyzer.ProcessUsers(userInfos, reasonsMap)

	// Process condo checker
	c.condoChecker.ProcessUsers(userInfos, reasonsMap)

	// Process outfit analysis (only for flagged users)
	c.outfitAnalyzer.ProcessOutfits(userInfos, reasonsMap)

	// Stop if no users were flagged
	if len(reasonsMap) == 0 {
		c.logger.Info("No flagged users found", zap.Int("userInfos", len(userInfos)))
		return
	}

	// Create final flagged users map
	flaggedUsers := make(map[uint64]*types.User, len(reasonsMap))
	for _, user := range userInfos {
		if reasons, ok := reasonsMap[user.ID]; ok {
			user.Reasons = reasons
			user.Confidence = utils.CalculateConfidence(reasons)
			flaggedUsers[user.ID] = user
		}
	}

	// Save flagged users to database
	if err := c.db.Service().User().SaveUsers(context.Background(), flaggedUsers); err != nil {
		c.logger.Error("Failed to save users", zap.Error(err))
	}

	// Track flagged users' group memberships
	go c.trackFlaggedUsersGroups(flaggedUsers)

	c.logger.Info("Finished processing users",
		zap.Int("totalProcessed", len(userInfos)),
		zap.Int("flaggedUsers", len(flaggedUsers)))
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
		if err := c.db.Model().Tracking().AddUsersToGroupsTracking(context.Background(), groupUsersTracking); err != nil {
			c.logger.Error("Failed to add flagged users to groups tracking", zap.Error(err))
		}
	}
}

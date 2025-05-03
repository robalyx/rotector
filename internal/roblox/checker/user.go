package checker

import (
	"context"

	apiTypes "github.com/jaxron/roapi.go/pkg/api/types"
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
	ivanAnalyzer   *ai.IvanAnalyzer
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
		ivanAnalyzer:   ai.NewIvanAnalyzer(app, logger),
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

// ProcessUsers runs users through multiple checking stages.
// Returns a map of flagged user IDs.
func (c *UserChecker) ProcessUsers(userInfos []*types.ReviewUser) map[uint64]struct{} {
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

	// Process ivan messages
	c.ivanAnalyzer.ProcessUsers(userInfos, reasonsMap)

	// Process outfit analysis
	flaggedOutfits := c.outfitAnalyzer.ProcessOutfits(userInfos, reasonsMap)

	// Stop if no users were flagged
	if len(reasonsMap) == 0 {
		c.logger.Info("No flagged users found", zap.Int("userInfos", len(userInfos)))
		return nil
	}

	// Create final flagged users map
	flaggedUsers := make(map[uint64]*types.ReviewUser, len(reasonsMap))
	flaggedStatus := make(map[uint64]struct{}, len(reasonsMap))

	for _, user := range userInfos {
		if reasons, ok := reasonsMap[user.ID]; ok {
			user.Reasons = reasons
			user.Confidence = utils.CalculateConfidence(reasons)
			flaggedUsers[user.ID] = user
			flaggedStatus[user.ID] = struct{}{}
		}
	}

	// Save flagged users to database
	if err := c.db.Service().User().SaveUsers(context.Background(), flaggedUsers); err != nil {
		c.logger.Error("Failed to save users", zap.Error(err))
	}

	// Track flagged users' group memberships
	go c.trackFlaggedUsersGroups(flaggedUsers)

	// Track flagged users' outfit assets
	go c.trackOutfitAssets(flaggedUsers, flaggedOutfits)

	c.logger.Info("Finished processing users",
		zap.Int("totalProcessed", len(userInfos)),
		zap.Int("flaggedUsers", len(flaggedUsers)))

	return flaggedStatus
}

// trackFlaggedUsersGroups adds flagged users' group memberships to tracking.
func (c *UserChecker) trackFlaggedUsersGroups(flaggedUsers map[uint64]*types.ReviewUser) {
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

// trackOutfitAssets adds outfit assets to tracking.
func (c *UserChecker) trackOutfitAssets(flaggedUsers map[uint64]*types.ReviewUser, flaggedOutfits map[uint64]map[string]struct{}) {
	assetOutfitsTracking := make(map[uint64][]types.TrackedID)

	// Collect outfit assets only for flagged outfits
	for userID, user := range flaggedUsers {
		// Skip if user wasn't flagged for outfit reasons
		if user.Reasons == nil || user.Reasons[enum.UserReasonTypeOutfit] == nil {
			continue
		}

		// Get the map of flagged outfits
		userFlaggedOutfits, hasFlaggedOutfits := flaggedOutfits[userID]
		if !hasFlaggedOutfits {
			continue
		}

		// Track current outfit assets if it was flagged
		if _, currentOutfitFlagged := userFlaggedOutfits["Current Outfit"]; currentOutfitFlagged && user.CurrentAssets != nil {
			for _, asset := range user.CurrentAssets {
				if isTrackableAssetType(asset.AssetType.ID) {
					assetOutfitsTracking[asset.ID] = append(assetOutfitsTracking[asset.ID], types.NewUserID(user.ID))
				}
			}
		}

		// Create map of outfit IDs to names for O(1) lookup
		outfitNames := make(map[uint64]string, len(user.Outfits))
		for _, outfit := range user.Outfits {
			outfitNames[outfit.ID] = outfit.Name
		}

		// Track assets from flagged outfits
		for outfitID, assets := range user.OutfitAssets {
			// Get outfit name from our map
			outfitName, exists := outfitNames[outfitID]
			if !exists || outfitName == "Current Outfit" {
				continue
			}

			// Skip if this outfit wasn't flagged
			if _, wasFlagged := userFlaggedOutfits[outfitName]; !wasFlagged {
				continue
			}

			// Track assets for this flagged outfit
			for _, asset := range assets {
				if isTrackableAssetType(asset.AssetType.ID) {
					assetOutfitsTracking[asset.ID] = append(assetOutfitsTracking[asset.ID], types.NewOutfitID(outfitID))
				}
			}
		}
	}

	// Add to tracking if we have any data
	if len(assetOutfitsTracking) > 0 {
		if err := c.db.Model().Tracking().AddOutfitAssetsToTracking(context.Background(), assetOutfitsTracking); err != nil {
			c.logger.Error("Failed to add outfit assets to tracking", zap.Error(err))
		}
	}
}

// isTrackableAssetType checks if an asset type is one we want to track.
func isTrackableAssetType(assetType apiTypes.ItemAssetType) bool {
	switch assetType {
	case apiTypes.ItemAssetTypeTShirt,
		apiTypes.ItemAssetTypeShirt,
		apiTypes.ItemAssetTypePants,
		apiTypes.ItemAssetTypeNeckAccessory,
		apiTypes.ItemAssetTypeShoulderAccessory,
		apiTypes.ItemAssetTypeFrontAccessory,
		apiTypes.ItemAssetTypeBackAccessory,
		apiTypes.ItemAssetTypeWaistAccessory,
		apiTypes.ItemAssetTypeEarAccessory,
		apiTypes.ItemAssetTypeEyeAccessory,
		apiTypes.ItemAssetTypeTShirtAccessory,
		apiTypes.ItemAssetTypeShirtAccessory,
		apiTypes.ItemAssetTypePantsAccessory,
		apiTypes.ItemAssetTypeJacketAccessory,
		apiTypes.ItemAssetTypeSweaterAccessory,
		apiTypes.ItemAssetTypeShortsAccessory,
		apiTypes.ItemAssetTypeLeftShoeAccessory,
		apiTypes.ItemAssetTypeRightShoeAccessory,
		apiTypes.ItemAssetTypeDressSkirtAccessory,
		apiTypes.ItemAssetTypeEyebrowAccessory,
		apiTypes.ItemAssetTypeEyelashAccessory:
		return true
	default:
		return false
	}
}

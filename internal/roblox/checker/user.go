package checker

import (
	"context"
	"sync"
	"time"

	apiTypes "github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/robalyx/rotector/internal/ai"
	"github.com/robalyx/rotector/internal/cloudflare"
	"github.com/robalyx/rotector/internal/database"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/database/types/enum"
	"github.com/robalyx/rotector/internal/roblox/fetcher"
	"github.com/robalyx/rotector/internal/setup"
	"github.com/robalyx/rotector/internal/translator"
	"github.com/robalyx/rotector/pkg/utils"
	"github.com/sourcegraph/conc/pool"
	"go.uber.org/zap"
)

// UserCheckerParams contains all the parameters needed for user checker processing.
type UserCheckerParams struct {
	Users                     []*types.ReviewUser         `json:"users"`
	ExistingUsers             map[int64]*types.ReviewUser `json:"existingUsers"`
	InappropriateOutfitFlags  map[int64]struct{}          `json:"inappropriateOutfitFlags"`
	InappropriateProfileFlags map[int64]struct{}          `json:"inappropriateProfileFlags"`
	InappropriateFriendsFlags map[int64]struct{}          `json:"inappropriateFriendsFlags"`
	InappropriateGroupsFlags  map[int64]struct{}          `json:"inappropriateGroupsFlags"`
}

// UserChecker coordinates the checking process by combining results from
// multiple checking methods (AI, groups, friends) and managing the progress bar.
type UserChecker struct {
	app                *setup.App
	db                 database.Client
	cfClient           *cloudflare.Client
	userFetcher        *fetcher.UserFetcher
	gameFetcher        *fetcher.GameFetcher
	outfitFetcher      *fetcher.OutfitFetcher
	translator         *translator.Translator
	userAnalyzer       *ai.UserAnalyzer
	userReasonAnalyzer *ai.UserReasonAnalyzer
	categoryAnalyzer   *ai.CategoryAnalyzer
	outfitAnalyzer     *ai.OutfitAnalyzer
	groupChecker       *GroupChecker
	friendChecker      *FriendChecker
	condoChecker       *CondoChecker
	logger             *zap.Logger
}

// NewUserChecker creates a UserChecker with all required dependencies.
func NewUserChecker(app *setup.App, userFetcher *fetcher.UserFetcher, logger *zap.Logger) *UserChecker {
	trans := translator.New(app.RoAPI.GetClient())

	return &UserChecker{
		app:                app,
		db:                 app.DB,
		cfClient:           app.CFClient,
		userFetcher:        userFetcher,
		gameFetcher:        fetcher.NewGameFetcher(app.RoAPI, logger),
		outfitFetcher:      fetcher.NewOutfitFetcher(app.RoAPI, logger),
		translator:         trans,
		userAnalyzer:       ai.NewUserAnalyzer(app, trans, logger),
		userReasonAnalyzer: ai.NewUserReasonAnalyzer(app, logger),
		categoryAnalyzer:   ai.NewCategoryAnalyzer(app, logger),
		outfitAnalyzer:     ai.NewOutfitAnalyzer(app, logger),
		groupChecker:       NewGroupChecker(app, logger),
		friendChecker:      NewFriendChecker(app, logger),
		condoChecker:       NewCondoChecker(app, logger),
		logger:             logger.Named("user_checker"),
	}
}

// ProcessResult contains the results of processing users.
type ProcessResult struct {
	FlaggedStatus  map[int64]struct{}          // User IDs that were flagged
	FlaggedUsers   map[int64]*types.ReviewUser // Full user data for flagged users (< 90% confidence)
	ConfirmedUsers map[int64]*types.ReviewUser // Full user data for auto-confirmed users (>= 90% confidence)
}

// ProcessUsers runs users through multiple checking stages.
// Returns processing results containing flagged user IDs and their full data.
func (c *UserChecker) ProcessUsers(ctx context.Context, params *UserCheckerParams) *ProcessResult {
	c.logger.Info("Processing users", zap.Int("userInfos", len(params.Users)))

	// Initialize map to store reasons
	reasonsMap := make(map[int64]types.Reasons[enum.UserReasonType])

	// Preserve manually-added reasons from existing users before analysis
	// This ensures moderator-added reasons (Chat, Favorites, Badges) are not lost during reprocessing
	if params.ExistingUsers != nil {
		for userID, existingUser := range params.ExistingUsers {
			if existingUser.Reasons == nil {
				continue
			}

			for reasonType, reason := range existingUser.Reasons {
				if !enum.IsAutoAnalyzedReason(reasonType) {
					if reasonsMap[userID] == nil {
						reasonsMap[userID] = make(types.Reasons[enum.UserReasonType])
					}

					reasonsMap[userID][reasonType] = reason
				}
			}
		}
	}

	// Create context with timeout
	ctxWithTimeout, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	// Prepare friend and group maps
	confirmedFriendsMap, flaggedFriendsMap := c.friendChecker.PrepareFriendMaps(ctxWithTimeout, params.Users)
	confirmedGroupsMap, flaggedGroupsMap, mixedGroupsMap := c.groupChecker.PrepareGroupMaps(ctxWithTimeout, params.Users)

	// Process friend checker
	c.friendChecker.ProcessUsers(ctxWithTimeout, &FriendCheckerParams{
		Users:                     params.Users,
		ReasonsMap:                reasonsMap,
		ConfirmedFriendsMap:       confirmedFriendsMap,
		FlaggedFriendsMap:         flaggedFriendsMap,
		ConfirmedGroupsMap:        confirmedGroupsMap,
		FlaggedGroupsMap:          flaggedGroupsMap,
		InappropriateFriendsFlags: params.InappropriateFriendsFlags,
	})

	// Process group checker
	c.groupChecker.ProcessUsers(ctxWithTimeout, &GroupCheckerParams{
		Users:                    params.Users,
		ReasonsMap:               reasonsMap,
		ConfirmedFriendsMap:      confirmedFriendsMap,
		FlaggedFriendsMap:        flaggedFriendsMap,
		ConfirmedGroupsMap:       confirmedGroupsMap,
		FlaggedGroupsMap:         flaggedGroupsMap,
		MixedGroupsMap:           mixedGroupsMap,
		InappropriateGroupsFlags: params.InappropriateGroupsFlags,
	})

	// Process condo checker
	if err := c.condoChecker.ProcessUsers(ctxWithTimeout, &CondoCheckerParams{
		Users:              params.Users,
		ReasonsMap:         reasonsMap,
		ConfirmedGroupsMap: confirmedGroupsMap,
	}); err != nil {
		c.logger.Error("Failed to process condo checker", zap.Error(err))
	}

	// Prepare user info maps
	translatedInfos, originalInfos := c.prepareUserInfoMaps(ctxWithTimeout, params.Users)

	// Process users through AI analysis
	acceptedUsers := c.userAnalyzer.ProcessUsers(ctxWithTimeout, &ai.ProcessUsersParams{
		Users:                     params.Users,
		TranslatedInfos:           translatedInfos,
		OriginalInfos:             originalInfos,
		ReasonsMap:                reasonsMap,
		ConfirmedFriendsMap:       confirmedFriendsMap,
		FlaggedFriendsMap:         flaggedFriendsMap,
		ConfirmedGroupsMap:        confirmedGroupsMap,
		FlaggedGroupsMap:          flaggedGroupsMap,
		MixedGroupsMap:            mixedGroupsMap,
		InappropriateProfileFlags: params.InappropriateProfileFlags,
		InappropriateFriendsFlags: params.InappropriateFriendsFlags,
		InappropriateGroupsFlags:  params.InappropriateGroupsFlags,
	})

	// Generate detailed reasons for all accepted users
	if len(acceptedUsers) > 0 {
		c.userReasonAnalyzer.ProcessFlaggedUsers(
			ctxWithTimeout, acceptedUsers, translatedInfos, originalInfos, reasonsMap, 0,
		)
	}

	// Process outfit analysis
	flaggedOutfits, furryUsers := c.outfitAnalyzer.ProcessUsers(ctxWithTimeout, &ai.OutfitAnalyzerParams{
		Users:                    params.Users,
		ReasonsMap:               reasonsMap,
		InappropriateOutfitFlags: params.InappropriateOutfitFlags,
	})

	// Remove friend-only flags for furry users to prevent false positive cascade
	c.removeFurryUserFriendOnlyFlags(reasonsMap, furryUsers)

	// Detect past offenders - users who were previously flagged/confirmed but now have no violations
	pastOffenderIDs := make([]int64, 0)

	for _, user := range params.Users {
		if _, hasReasons := reasonsMap[user.ID]; !hasReasons {
			if existingUser, exists := params.ExistingUsers[user.ID]; exists {
				if existingUser.Status == enum.UserTypeFlagged || existingUser.Status == enum.UserTypeConfirmed {
					// User has no reasons after processing
					// They were previously flagged/confirmed, they became clean (past offender)
					pastOffenderIDs = append(pastOffenderIDs, user.ID)
					c.logger.Info("User became clean, will update to past offender",
						zap.Int64("userID", user.ID),
						zap.String("previousStatus", existingUser.Status.String()))
				}
			}
		}
	}

	// Update past offenders
	c.handlePastOffenders(ctx, pastOffenderIDs)

	// Stop if no users were flagged
	if len(reasonsMap) == 0 {
		c.logger.Info("No flagged users found", zap.Int("userInfos", len(params.Users)))

		return &ProcessResult{
			FlaggedStatus:  make(map[int64]struct{}),
			FlaggedUsers:   make(map[int64]*types.ReviewUser),
			ConfirmedUsers: make(map[int64]*types.ReviewUser),
		}
	}

	// Create final flagged users maps
	flaggedUsers := make(map[int64]*types.ReviewUser, len(reasonsMap))
	confirmedUsers := make(map[int64]*types.ReviewUser)
	flaggedStatus := make(map[int64]struct{}, len(reasonsMap))

	for _, user := range params.Users {
		if reasons, ok := reasonsMap[user.ID]; ok {
			// User has reasons and are flagged
			user.Reasons = reasons
			user.Confidence = utils.CalculateConfidence(reasons)
			flaggedUsers[user.ID] = user
			flaggedStatus[user.ID] = struct{}{}
		}
	}

	// Classify flagged users into violation categories
	c.classifyFlaggedUsers(ctxWithTimeout, flaggedUsers)

	// Save flagged users to database
	if err := c.db.Service().User().SaveUsers(ctx, flaggedUsers); err != nil {
		c.logger.Error("Failed to save users", zap.Error(err))
	}

	// Automatically confirm users with high confidence scores
	c.autoConfirmHighConfidenceUsers(ctx, flaggedUsers, confirmedUsers)

	// Synchronize user status to external D1 database for API access
	c.syncFlaggedUsersToD1(ctx, flaggedUsers, confirmedUsers)

	// Track flagged users' group memberships
	go c.trackFlaggedUsersGroups(ctx, flaggedUsers)

	// Track flagged users' outfit assets
	go c.trackOutfitAssets(ctx, flaggedUsers, flaggedOutfits)

	// Track flagged users' favorite games
	go c.trackFavoriteGames(ctx, flaggedUsers)

	c.logger.Info("Finished processing users",
		zap.Int("totalProcessed", len(params.Users)),
		zap.Int("flaggedUsers", len(flaggedUsers)),
		zap.Int("confirmedUsers", len(confirmedUsers)))

	return &ProcessResult{
		FlaggedStatus:  flaggedStatus,
		FlaggedUsers:   flaggedUsers,
		ConfirmedUsers: confirmedUsers,
	}
}

// prepareUserInfoMaps creates maps of user information for both translated and original content.
func (c *UserChecker) prepareUserInfoMaps(
	ctx context.Context, userInfos []*types.ReviewUser,
) (map[string]*types.ReviewUser, map[string]*types.ReviewUser) {
	var (
		originalInfos   = make(map[string]*types.ReviewUser)
		translatedInfos = make(map[string]*types.ReviewUser)
		p               = pool.New().WithContext(ctx).WithMaxGoroutines(50)
		mu              sync.Mutex
	)

	// Initialize maps and spawn translation goroutines
	for _, info := range userInfos {
		originalInfos[info.Name] = info

		p.Go(func(ctx context.Context) error {
			// Skip empty descriptions
			if info.Description == "" {
				mu.Lock()

				translatedInfos[info.Name] = info

				mu.Unlock()

				return nil
			}

			// Translate the description with retry
			var translated string

			err := utils.WithRetry(ctx, func() error {
				var err error

				translated, err = c.translator.Translate(
					ctx,
					info.Description,
					"auto", // Auto-detect source language
					"en",   // Translate to English
				)

				return err
			}, utils.GetAIRetryOptions())
			if err != nil {
				// Use original userInfo if translation fails
				mu.Lock()

				translatedInfos[info.Name] = info

				mu.Unlock()
				c.logger.Error("Translation failed, using original description",
					zap.String("username", info.Name),
					zap.Error(err))

				return nil
			}

			// Create new Info with translated description
			translatedInfo := *info
			if translatedInfo.Description != translated {
				translatedInfo.Description = translated

				c.logger.Debug("Translated description", zap.String("username", info.Name))
			}

			mu.Lock()

			translatedInfos[info.Name] = &translatedInfo

			mu.Unlock()

			return nil
		})
	}

	// Wait for all translations to complete
	if err := p.Wait(); err != nil {
		c.logger.Error("Error during translations", zap.Error(err))
	}

	return translatedInfos, originalInfos
}

// removeFurryUserFriendOnlyFlags removes friend-only flags for furry users to prevent false positive cascade.
func (c *UserChecker) removeFurryUserFriendOnlyFlags(
	reasonsMap map[int64]types.Reasons[enum.UserReasonType], furryUsers map[int64]struct{},
) {
	for userID := range furryUsers {
		if reasons, exists := reasonsMap[userID]; exists {
			// Only remove if user has exactly one reason and it's friend-only
			if len(reasons) == 1 {
				if _, hasFriendReason := reasons[enum.UserReasonTypeFriend]; hasFriendReason {
					delete(reasonsMap, userID)
					c.logger.Info("Removed friend-only flag for furry user to prevent cascade",
						zap.Int64("userID", userID))
				}
			}
		}
	}
}

// classifyFlaggedUsers classifies users into violation categories.
func (c *UserChecker) classifyFlaggedUsers(ctx context.Context, flaggedUsers map[int64]*types.ReviewUser) {
	if len(flaggedUsers) == 0 {
		return
	}

	c.logger.Info("Classifying users into violation categories",
		zap.Int("totalUsers", len(flaggedUsers)))

	categoryResults := c.categoryAnalyzer.ClassifyUsers(ctx, flaggedUsers, 0)

	// Apply categories to users
	for userID, category := range categoryResults {
		if user, exists := flaggedUsers[userID]; exists {
			user.Category = category
			c.logger.Debug("Assigned category to user",
				zap.Int64("userID", userID),
				zap.String("username", user.Name),
				zap.String("category", category.String()))
		}
	}

	c.logger.Info("Finished category classification",
		zap.Int("classified", len(categoryResults)))
}

// handlePastOffenders updates users who became clean to past offender status.
func (c *UserChecker) handlePastOffenders(ctx context.Context, pastOffenderIDs []int64) {
	if len(pastOffenderIDs) == 0 {
		return
	}

	if err := c.db.Service().User().UpdateToPastOffender(ctx, pastOffenderIDs); err != nil {
		c.logger.Error("Failed to update users to past offender status", zap.Error(err))
	} else {
		c.logger.Info("Updated users to past offender status",
			zap.Int("count", len(pastOffenderIDs)))
	}

	if err := c.cfClient.UserFlags.UpdateToPastOffender(ctx, pastOffenderIDs); err != nil {
		c.logger.Error("Failed to update D1 users to past offender status", zap.Error(err))
	}
}

// autoConfirmHighConfidenceUsers identifies and confirms users meeting auto-confirmation criteria.
func (c *UserChecker) autoConfirmHighConfidenceUsers(
	ctx context.Context, flaggedUsers, confirmedUsers map[int64]*types.ReviewUser,
) {
	var usersToConfirm []*types.ReviewUser

	for _, user := range flaggedUsers {
		if c.meetsAutoConfirmationCriteria(user) {
			usersToConfirm = append(usersToConfirm, user)
			confirmedUsers[user.ID] = user
			delete(flaggedUsers, user.ID)
		}
	}

	if len(usersToConfirm) == 0 {
		return
	}

	if err := c.db.Service().User().ConfirmUsers(ctx, usersToConfirm, 0); err != nil {
		c.logger.Error("Failed to auto-confirm high-confidence users",
			zap.Int("count", len(usersToConfirm)),
			zap.Error(err))
	} else {
		c.logger.Info("Auto-confirmed high-confidence users",
			zap.Int("count", len(usersToConfirm)))
	}
}

// syncFlaggedUsersToD1 synchronizes flagged and confirmed users to the D1 database for API access.
func (c *UserChecker) syncFlaggedUsersToD1(
	ctx context.Context, flaggedUsers map[int64]*types.ReviewUser, confirmedUsers map[int64]*types.ReviewUser,
) {
	if len(flaggedUsers) > 0 {
		if err := c.cfClient.UserFlags.AddFlagged(ctx, flaggedUsers); err != nil {
			c.logger.Error("Failed to add flagged users to D1", zap.Error(err))
		}
	}

	if len(confirmedUsers) > 0 {
		for _, user := range confirmedUsers {
			if err := c.cfClient.UserFlags.AddConfirmed(ctx, user, 0); err != nil {
				c.logger.Error("Failed to add auto-confirmed user to D1",
					zap.Error(err),
					zap.Int64("userID", user.ID))
			}
		}
	}
}

// trackFlaggedUsersGroups adds flagged users' group memberships to tracking.
func (c *UserChecker) trackFlaggedUsersGroups(ctx context.Context, flaggedUsers map[int64]*types.ReviewUser) {
	groupUsersTracking := make(map[int64][]int64)

	// Collect group memberships for flagged users
	for userID, user := range flaggedUsers {
		for _, group := range user.Groups {
			if group.Group.MemberCount > 0 && group.Group.MemberCount <= c.app.Config.Worker.ThresholdLimits.MaxGroupMembersTrack {
				groupUsersTracking[group.Group.ID] = append(groupUsersTracking[group.Group.ID], userID)
			}
		}
	}

	// Add to tracking if we have any data
	// NOTE: No need to check exclusions using GetExcludedGroupIDs here because new users
	// joining these groups likely means we shouldn't have excluded them
	if len(groupUsersTracking) > 0 {
		if err := c.db.Model().Tracking().AddUsersToGroupsTracking(ctx, groupUsersTracking); err != nil {
			c.logger.Error("Failed to add flagged users to groups tracking", zap.Error(err))
		}
	}
}

// trackOutfitAssets adds outfit assets to tracking.
func (c *UserChecker) trackOutfitAssets(
	ctx context.Context, flaggedUsers map[int64]*types.ReviewUser, flaggedOutfits map[int64]map[string]struct{},
) {
	assetOutfitsTracking := make(map[int64][]types.TrackedID)

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
		outfitNames := make(map[int64]string, len(user.Outfits))
		for _, outfit := range user.Outfits {
			outfitNames[outfit.ID] = outfit.Name
		}

		// Get outfit details for flagged outfits
		if len(user.Outfits) > 0 {
			outfitAssets, err := c.outfitFetcher.GetOutfitDetails(ctx, user.Outfits)
			if err != nil {
				c.logger.Error("Failed to fetch outfit details",
					zap.Error(err),
					zap.Int64("userID", user.ID))

				continue
			}

			// Track assets from flagged outfits
			for outfitID, assets := range outfitAssets {
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
	}

	// Add to tracking if we have any data
	if len(assetOutfitsTracking) > 0 {
		if err := c.db.Model().Tracking().AddOutfitAssetsToTracking(ctx, assetOutfitsTracking); err != nil {
			c.logger.Error("Failed to add outfit assets to tracking", zap.Error(err))
		}
	}
}

// trackFavoriteGames adds flagged users' favorite games to tracking.
func (c *UserChecker) trackFavoriteGames(ctx context.Context, flaggedUsers map[int64]*types.ReviewUser) {
	gameUsersTracking := make(map[int64][]int64)

	// Fetch favorite games for each flagged user
	for userID := range flaggedUsers {
		favorites, err := c.gameFetcher.FetchFavoriteGames(ctx, userID)
		if err != nil {
			c.logger.Error("Failed to fetch favorite games for user",
				zap.Int64("userID", userID),
				zap.Error(err))

			continue
		}

		// Track games that meet the visit threshold
		for _, game := range favorites {
			if game.PlaceVisits <= c.app.Config.Worker.ThresholdLimits.MaxGameVisitsTrack {
				// NOTE: just realized we're using the universe ID and not the root place ID
				// RIP!!!!!! wtf roblox
				gameUsersTracking[game.ID] = append(gameUsersTracking[game.ID], userID)
			}
		}
	}

	// Add to tracking if we have any data
	if len(gameUsersTracking) > 0 {
		if err := c.db.Model().Tracking().AddGamesToTracking(ctx, gameUsersTracking); err != nil {
			c.logger.Error("Failed to add flagged users to games tracking", zap.Error(err))
		}
	}
}

// meetsAutoConfirmationCriteria validates eligibility for automatic user confirmation.
func (c *UserChecker) meetsAutoConfirmationCriteria(user *types.ReviewUser) bool {
	if user.Confidence < 0.90 || user.Reasons == nil || len(user.Reasons) < 2 {
		return false
	}

	hasProfileReason := user.Reasons[enum.UserReasonTypeProfile] != nil
	hasFriendReason := user.Reasons[enum.UserReasonTypeFriend] != nil
	hasGroupReason := user.Reasons[enum.UserReasonTypeGroup] != nil
	hasOutfitReason := user.Reasons[enum.UserReasonTypeOutfit] != nil

	// Users with many different reason types have strong evidence
	if len(user.Reasons) > 3 {
		return true
	}

	// Check if user meets profile+group criteria
	hasProfileAndGroup := hasProfileReason && hasGroupReason && !hasOutfitReason
	if hasProfileAndGroup {
		profileReason := user.Reasons[enum.UserReasonTypeProfile]
		if profileReason.Confidence < 0.70 {
			return false
		}
	}

	// Check if user meets friend+group criteria
	hasFriendAndGroup := hasFriendReason && hasGroupReason

	return hasProfileAndGroup || hasFriendAndGroup
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

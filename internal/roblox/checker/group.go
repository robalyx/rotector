package checker

import (
	"context"
	"math"
	"time"

	apiTypes "github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/robalyx/rotector/internal/ai"
	"github.com/robalyx/rotector/internal/database"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/database/types/enum"
	"github.com/robalyx/rotector/internal/setup"
	"go.uber.org/zap"
)

// GroupCheckerParams contains all the parameters needed for group checker processing.
type GroupCheckerParams struct {
	Users                     []*types.ReviewUser                           `json:"users"`
	ReasonsMap                map[uint64]types.Reasons[enum.UserReasonType] `json:"reasonsMap"`
	ConfirmedFriendsMap       map[uint64]map[uint64]*types.ReviewUser       `json:"confirmedFriendsMap"`
	FlaggedFriendsMap         map[uint64]map[uint64]*types.ReviewUser       `json:"flaggedFriendsMap"`
	ConfirmedGroupsMap        map[uint64]map[uint64]*types.ReviewGroup      `json:"confirmedGroupsMap"`
	FlaggedGroupsMap          map[uint64]map[uint64]*types.ReviewGroup      `json:"flaggedGroupsMap"`
	InappropriateGroupsFlags  map[uint64]struct{}                           `json:"inappropriateGroupsFlags"`
}

// GroupCheckResult contains the result of checking a user's groups.
type GroupCheckResult struct {
	UserID      uint64
	User        *types.User
	AutoFlagged bool
}

// GroupChecker handles the checking of user groups by comparing them against
// a database of known inappropriate groups.
type GroupChecker struct {
	db                   database.Client
	groupReasonAnalyzer  *ai.GroupReasonAnalyzer
	logger               *zap.Logger
	maxGroupMembersTrack uint64
	minFlaggedOverride   int
	minFlaggedPercentage float64
}

// NewGroupChecker creates a GroupChecker with database access for looking up
// flagged group information.
func NewGroupChecker(app *setup.App, logger *zap.Logger) *GroupChecker {
	return &GroupChecker{
		db:                   app.DB,
		groupReasonAnalyzer:  ai.NewGroupReasonAnalyzer(app, logger),
		logger:               logger.Named("group_checker"),
		maxGroupMembersTrack: app.Config.Worker.ThresholdLimits.MaxGroupMembersTrack,
		minFlaggedOverride:   app.Config.Worker.ThresholdLimits.MinFlaggedOverride,
		minFlaggedPercentage: app.Config.Worker.ThresholdLimits.MinFlaggedPercentage,
	}
}

// CheckGroupPercentages analyzes groups to find those exceeding the flagged user threshold.
func (c *GroupChecker) CheckGroupPercentages(
	ctx context.Context, groupInfos []*apiTypes.GroupResponse, groupToFlaggedUsers map[uint64][]uint64,
) map[uint64]*types.ReviewGroup {
	flaggedGroups := make(map[uint64]*types.ReviewGroup)
	largeGroupIDs := make([]uint64, 0)

	// Identify groups that exceed thresholds
	for _, groupInfo := range groupInfos {
		// Skip groups that are too large to track
		if groupInfo.MemberCount > c.maxGroupMembersTrack {
			largeGroupIDs = append(largeGroupIDs, groupInfo.ID)
			continue
		}

		flaggedUsers := groupToFlaggedUsers[groupInfo.ID]

		var reason string

		// Calculate percentage of flagged users
		percentage := (float64(len(flaggedUsers)) / float64(groupInfo.MemberCount)) * 100

		// Determine if group should be flagged
		switch {
		case len(flaggedUsers) >= c.minFlaggedOverride:
			reason = "Group has large number of flagged users"
		case percentage >= c.minFlaggedPercentage:
			reason = "Group has large percentage of flagged users"
		default:
			continue
		}

		now := time.Now()
		flaggedGroups[groupInfo.ID] = &types.ReviewGroup{
			Group: &types.Group{
				ID:            groupInfo.ID,
				Name:          groupInfo.Name,
				Description:   groupInfo.Description,
				Owner:         groupInfo.Owner,
				Shout:         groupInfo.Shout,
				LastUpdated:   now,
				LastLockCheck: now,
			},
			Reasons: types.Reasons[enum.GroupReasonType]{
				enum.GroupReasonTypeMember: &types.Reason{
					Message:    reason,
					Confidence: 0, // NOTE: Confidence will be updated later
				},
			},
		}
	}

	// Remove large groups from tracking
	if len(largeGroupIDs) > 0 {
		if err := c.db.Model().Tracking().RemoveGroupsFromTracking(ctx, largeGroupIDs); err != nil {
			c.logger.Error("Failed to remove large groups from tracking",
				zap.Error(err),
				zap.Uint64s("groupIDs", largeGroupIDs))
		} else {
			c.logger.Info("Removed large groups from tracking",
				zap.Int("count", len(largeGroupIDs)))
		}
	}

	// If no groups were flagged, return empty map
	if len(flaggedGroups) == 0 {
		return flaggedGroups
	}

	// Collect all unique flagged user IDs
	allFlaggedUserIDs := make([]uint64, 0)

	for groupID := range flaggedGroups {
		if flaggedUsers, ok := groupToFlaggedUsers[groupID]; ok {
			allFlaggedUserIDs = append(allFlaggedUserIDs, flaggedUsers...)
		}
	}

	// Get user data for confidence calculation
	users, err := c.db.Model().User().GetUsersByIDs(
		ctx, allFlaggedUserIDs, types.UserFieldBasic|types.UserFieldConfidence,
	)
	if err != nil {
		c.logger.Error("Failed to get user confidence data", zap.Error(err))
		return flaggedGroups
	}

	// Calculate average confidence for each flagged group
	for groupID, group := range flaggedGroups {
		group.Confidence = c.calculateGroupConfidence(groupToFlaggedUsers[groupID], users)
		if memberReason, ok := group.Reasons[enum.GroupReasonTypeMember]; ok {
			memberReason.Confidence = group.Confidence
		}
	}

	return flaggedGroups
}

// PrepareGroupMaps handles the preparation of confirmed and flagged group maps.
func (c *GroupChecker) PrepareGroupMaps(
	ctx context.Context, userInfos []*types.ReviewUser,
) (map[uint64]map[uint64]*types.ReviewGroup, map[uint64]map[uint64]*types.ReviewGroup) {
	confirmedGroupsMap := make(map[uint64]map[uint64]*types.ReviewGroup)
	flaggedGroupsMap := make(map[uint64]map[uint64]*types.ReviewGroup)

	// Collect all unique group IDs across all users
	uniqueGroupIDs := make(map[uint64]struct{})

	for _, userInfo := range userInfos {
		for _, group := range userInfo.Groups {
			uniqueGroupIDs[group.Group.ID] = struct{}{}
		}
	}

	// Convert unique IDs to slice
	groupIDs := make([]uint64, 0, len(uniqueGroupIDs))
	for groupID := range uniqueGroupIDs {
		groupIDs = append(groupIDs, groupID)
	}

	// Fetch all existing groups if we have any
	var existingGroups map[uint64]*types.ReviewGroup

	if len(groupIDs) > 0 {
		var err error

		existingGroups, err = c.db.Model().Group().GetGroupsByIDs(
			ctx, groupIDs, types.GroupFieldBasic|types.GroupFieldReasons,
		)
		if err != nil {
			c.logger.Error("Failed to fetch existing groups", zap.Error(err))

			existingGroups = make(map[uint64]*types.ReviewGroup)
		}
	} else {
		existingGroups = make(map[uint64]*types.ReviewGroup)
	}

	// Prepare maps for confirmed and flagged groups per user
	for _, userInfo := range userInfos {
		confirmedGroups := make(map[uint64]*types.ReviewGroup)
		flaggedGroups := make(map[uint64]*types.ReviewGroup)

		for _, group := range userInfo.Groups {
			if reviewGroup, exists := existingGroups[group.Group.ID]; exists {
				switch reviewGroup.Status {
				case enum.GroupTypeConfirmed:
					confirmedGroups[group.Group.ID] = reviewGroup
				case enum.GroupTypeFlagged:
					flaggedGroups[group.Group.ID] = reviewGroup
				default:
					continue
				}
			}
		}

		confirmedGroupsMap[userInfo.ID] = confirmedGroups
		flaggedGroupsMap[userInfo.ID] = flaggedGroups
	}

	return confirmedGroupsMap, flaggedGroupsMap
}

// ProcessUsers checks multiple users' groups concurrently and updates reasonsMap.
func (c *GroupChecker) ProcessUsers(ctx context.Context, params *GroupCheckerParams) {
	// Track counts before processing
	existingFlags := len(params.ReasonsMap)

	// Track users that exceed confidence threshold
	var usersToAnalyze []*types.ReviewUser

	userConfidenceMap := make(map[uint64]float64)

	// Process results
	for _, userInfo := range params.Users {
		confirmedCount := len(params.ConfirmedGroupsMap[userInfo.ID])
		flaggedCount := len(params.FlaggedGroupsMap[userInfo.ID])
		totalInappropriate := confirmedCount + flaggedCount

		// Get total count of inappropriate friends
		totalInappropriateFriends, confirmedFriendCount := c.getInappropriateFriendCount(userInfo.ID, params.ConfirmedFriendsMap, params.FlaggedFriendsMap)

		// Skip users with only 1 inappropriate group unless they have inappropriate friends
		hasInappropriateFriendEvidence := totalInappropriateFriends > 5 ||
			(len(userInfo.Friends) > 0 && confirmedFriendCount > 0 && totalInappropriateFriends == confirmedFriendCount)

		if totalInappropriate == 1 && !hasInappropriateFriendEvidence {
			continue
		}

		// Calculate confidence score
		_, isInappropriateGroups := params.InappropriateGroupsFlags[userInfo.ID]
		confidence := c.calculateConfidence(confirmedCount, flaggedCount, len(userInfo.Groups), isInappropriateGroups)

		userConfidenceMap[userInfo.ID] = confidence

		// Check if user meets group threshold
		threshold := 0.5
		if _, isInappropriateGroups := params.InappropriateGroupsFlags[userInfo.ID]; isInappropriateGroups {
			threshold = 0.4
		}

		meetsGroupThreshold := confidence >= threshold

		// Check friend requirement for users with insufficient group evidence
		meetsFriendRequirement := c.evaluateFriendRequirement(userInfo, confirmedCount, flaggedCount, totalInappropriateFriends)

		// Only process users that meet both group threshold AND friend requirement
		if meetsGroupThreshold && meetsFriendRequirement {
			usersToAnalyze = append(usersToAnalyze, userInfo)
		}
	}

	// Generate AI reasons if we have users to analyze
	if len(usersToAnalyze) > 0 {
		// Generate reasons for flagged users
		reasons := c.groupReasonAnalyzer.GenerateGroupReasons(ctx, usersToAnalyze, params.ConfirmedGroupsMap, params.FlaggedGroupsMap)

		// Process results and update reasonsMap
		for _, userInfo := range usersToAnalyze {
			confirmedCount := len(params.ConfirmedGroupsMap[userInfo.ID])
			flaggedCount := len(params.FlaggedGroupsMap[userInfo.ID])
			confidence := userConfidenceMap[userInfo.ID]

			reason := reasons[userInfo.ID]
			if reason == "" {
				// Fallback to default reason format if AI generation failed
				reason = "Member of multiple inappropriate groups."
			}

			// Add new reason to reasons map
			if _, exists := params.ReasonsMap[userInfo.ID]; !exists {
				params.ReasonsMap[userInfo.ID] = make(types.Reasons[enum.UserReasonType])
			}

			params.ReasonsMap[userInfo.ID].Add(enum.UserReasonTypeGroup, &types.Reason{
				Message:    reason,
				Confidence: confidence,
			})

			c.logger.Debug("User flagged for group membership",
				zap.Uint64("userID", userInfo.ID),
				zap.Int("confirmedGroups", confirmedCount),
				zap.Int("flaggedGroups", flaggedCount),
				zap.Float64("confidence", confidence),
				zap.String("reason", reason))
		}
	}

	c.logger.Info("Finished processing groups",
		zap.Int("totalUsers", len(params.Users)),
		zap.Int("analyzedUsers", len(usersToAnalyze)),
		zap.Int("newFlags", len(params.ReasonsMap)-existingFlags))
}

// calculateGroupConfidence computes the confidence score for a group based on its flagged users.
func (c *GroupChecker) calculateGroupConfidence(flaggedUsers []uint64, users map[uint64]*types.ReviewUser) float64 {
	var (
		totalConfidence float64
		validUserCount  int
	)

	for _, userID := range flaggedUsers {
		if user, exists := users[userID]; exists {
			totalConfidence += user.Confidence
			validUserCount++
		}
	}

	if validUserCount == 0 {
		c.logger.Fatal("Unreachable: No valid users found for group")
		return 0.50
	}

	// Calculate average confidence
	avgConfidence := totalConfidence / float64(validUserCount)

	// Apply 20% boost if group exceeds override threshold
	if len(flaggedUsers) >= c.minFlaggedOverride {
		avgConfidence *= 1.2
	}

	// Clamp confidence between 0 and 1
	avgConfidence = math.Min(avgConfidence, 1.0)

	// Round confidence to 2 decimal places
	return math.Round(avgConfidence*100) / 100
}

// calculateConfidence computes a weighted confidence score based on group memberships.
func (c *GroupChecker) calculateConfidence(confirmedCount, flaggedCount, totalGroups int, enhanced bool) float64 {
	totalWeight := float64(confirmedCount) + (float64(flaggedCount) * 0.4)

	// Hard thresholds for serious cases
	if confirmedCount >= 8 || totalWeight >= 10 {
		return 1.0
	}

	// Absolute count bonuses to prevent gaming large networks
	var absoluteBonus float64

	switch {
	case confirmedCount >= 5:
		absoluteBonus = 0.4
	case confirmedCount >= 3:
		absoluteBonus = 0.2
	case confirmedCount >= 2:
		absoluteBonus = 0.1
	}

	// Calculate thresholds based on network size
	var adjustedThreshold float64

	switch {
	case totalGroups >= 100:
		adjustedThreshold = math.Max(6.0, 0.065*float64(totalGroups))
	case totalGroups >= 75:
		adjustedThreshold = math.Max(5.0, 0.07*float64(totalGroups))
	case totalGroups >= 50:
		adjustedThreshold = math.Max(4.5, 0.08*float64(totalGroups))
	case totalGroups >= 35:
		adjustedThreshold = math.Max(4.0, 0.10*float64(totalGroups))
	case totalGroups >= 20:
		adjustedThreshold = math.Max(3.5, 0.12*float64(totalGroups))
	case totalGroups >= 15:
		adjustedThreshold = math.Max(3.0, 0.14*float64(totalGroups))
	case totalGroups >= 10:
		adjustedThreshold = math.Max(2.5, 0.15*float64(totalGroups))
	case totalGroups >= 5:
		adjustedThreshold = math.Max(2.0, 0.20*float64(totalGroups))
	default:
		adjustedThreshold = math.Max(1.5, 0.25*float64(totalGroups))
	}

	weightedThreshold := adjustedThreshold * 1.3
	confirmedRatio := float64(confirmedCount) / adjustedThreshold
	weightedRatio := totalWeight / weightedThreshold
	maxRatio := math.Max(confirmedRatio, weightedRatio)

	var baseConfidence float64

	switch {
	case maxRatio >= 1.5:
		baseConfidence = 1.0
	case maxRatio >= 1.2:
		baseConfidence = 0.8
	case maxRatio >= 1.0:
		baseConfidence = 0.6
	case maxRatio >= 0.7:
		baseConfidence = 0.4
	case maxRatio >= 0.4:
		baseConfidence = 0.2
	default:
		baseConfidence = 0.0
	}

	finalConfidence := math.Min(baseConfidence+absoluteBonus, 1.0)

	if enhanced {
		finalConfidence = math.Min(finalConfidence*1.2, 1.0)
	}

	return finalConfidence
}

// evaluateFriendRequirement checks if a user meets the friend requirement based on group evidence.
// Returns true if the user meets the friend requirement or if friend requirement doesn't apply.
func (c *GroupChecker) evaluateFriendRequirement(
	userInfo *types.ReviewUser, confirmedCount, flaggedCount, totalInappropriateFriends int,
) bool {
	totalInappropriate := confirmedCount + flaggedCount
	totalWeight := float64(confirmedCount) + (float64(flaggedCount) * 0.8)

	// Special case for possible false positives
	if confirmedCount < 5 && len(userInfo.Groups) > 15 {
		if len(userInfo.Friends) > 5 {
			return totalInappropriateFriends >= 4
		}

		return totalInappropriateFriends >= 2
	}

	// Apply friend requirement if user has < 3 total weight unless all their groups are inappropriate
	shouldApplyFriendRequirement := totalWeight < 3.0 && totalInappropriate != len(userInfo.Groups)
	if !shouldApplyFriendRequirement {
		return true
	}

	requiredFlaggedFriends := c.getRequiredFlaggedFriends(len(userInfo.Friends))
	meetsFriendRequirement := totalInappropriateFriends >= requiredFlaggedFriends

	return meetsFriendRequirement
}

// getInappropriateFriendCount returns the total count of inappropriate and confirmed friends count for a user.
func (c *GroupChecker) getInappropriateFriendCount(
	userID uint64, confirmedFriendsMap, flaggedFriendsMap map[uint64]map[uint64]*types.ReviewUser,
) (confirmedFriendCount int, flaggedFriendCount int) {
	if confirmedFriends, exists := confirmedFriendsMap[userID]; exists {
		confirmedFriendCount = len(confirmedFriends)
	}

	if flaggedFriends, exists := flaggedFriendsMap[userID]; exists {
		flaggedFriendCount = len(flaggedFriends)
	}

	return confirmedFriendCount + flaggedFriendCount, confirmedFriendCount
}

// getRequiredFlaggedFriends returns the minimum number of flagged friends required based on total friend count.
func (c *GroupChecker) getRequiredFlaggedFriends(totalFriends int) int {
	switch {
	case totalFriends < 10:
		return 1
	case totalFriends < 25:
		return 3
	case totalFriends < 50:
		return 5
	case totalFriends < 100:
		return 8
	case totalFriends < 200:
		return 10
	default:
		return 12
	}
}

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

// ProcessUsers checks multiple users' groups concurrently and updates reasonsMap.
func (c *GroupChecker) ProcessUsers(
	ctx context.Context, userInfos []*types.ReviewUser, reasonsMap map[uint64]types.Reasons[enum.UserReasonType],
) {
	// Track counts before processing
	existingFlags := len(reasonsMap)

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

	// Fetch all existing groups
	existingGroups, err := c.db.Model().Group().GetGroupsByIDs(
		ctx, groupIDs, types.GroupFieldBasic|types.GroupFieldReasons,
	)
	if err != nil {
		c.logger.Error("Failed to fetch existing groups", zap.Error(err))
		return
	}

	// Prepare maps for confirmed and flagged groups per user
	confirmedGroupsMap := make(map[uint64]map[uint64]*types.ReviewGroup)
	flaggedGroupsMap := make(map[uint64]map[uint64]*types.ReviewGroup)

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

	// Track users that exceed confidence threshold
	var usersToAnalyze []*types.ReviewUser

	// Process results
	for _, userInfo := range userInfos {
		confirmedCount := len(confirmedGroupsMap[userInfo.ID])
		flaggedCount := len(flaggedGroupsMap[userInfo.ID])

		// Calculate confidence score
		confidence := c.calculateConfidence(confirmedCount, flaggedCount, len(userInfo.Groups))

		// Only process users that exceed threshold
		if confidence >= 0.5 {
			usersToAnalyze = append(usersToAnalyze, userInfo)
		}
	}

	// Generate AI reasons if we have users to analyze
	if len(usersToAnalyze) > 0 {
		// Generate reasons for flagged users
		reasons := c.groupReasonAnalyzer.GenerateGroupReasons(ctx, usersToAnalyze, confirmedGroupsMap, flaggedGroupsMap)

		// Process results and update reasonsMap
		for _, userInfo := range usersToAnalyze {
			confirmedCount := len(confirmedGroupsMap[userInfo.ID])
			flaggedCount := len(flaggedGroupsMap[userInfo.ID])
			confidence := c.calculateConfidence(confirmedCount, flaggedCount, len(userInfo.Groups))

			reason := reasons[userInfo.ID]
			if reason == "" {
				// Fallback to default reason format if AI generation failed
				reason = "Member of multiple inappropriate groups."
			}

			// Add new reason to reasons map
			if _, exists := reasonsMap[userInfo.ID]; !exists {
				reasonsMap[userInfo.ID] = make(types.Reasons[enum.UserReasonType])
			}
			reasonsMap[userInfo.ID].Add(enum.UserReasonTypeGroup, &types.Reason{
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
		zap.Int("totalUsers", len(userInfos)),
		zap.Int("analyzedUsers", len(usersToAnalyze)),
		zap.Int("newFlags", len(reasonsMap)-existingFlags))
}

// calculateGroupConfidence computes the confidence score for a group based on its flagged users.
func (c *GroupChecker) calculateGroupConfidence(flaggedUsers []uint64, users map[uint64]*types.ReviewUser) float64 {
	var totalConfidence float64
	var validUserCount int

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
func (c *GroupChecker) calculateConfidence(confirmedCount, flaggedCount, totalGroups int) float64 {
	var confidence float64

	// Factor 1: Absolute number of inappropriate groups - 50% weight
	inappropriateWeight := c.calculateInappropriateWeight(confirmedCount, flaggedCount)
	confidence += inappropriateWeight * 0.50

	// Factor 2: Ratio of inappropriate groups - 50% weight
	if totalGroups > 0 {
		totalInappropriate := float64(confirmedCount) + (float64(flaggedCount) * 0.5)
		ratioWeight := math.Min(totalInappropriate/float64(totalGroups), 1.0)
		confidence += ratioWeight * 0.50
	}

	return confidence
}

// calculateInappropriateWeight returns a weight based on the total number of inappropriate groups.
func (c *GroupChecker) calculateInappropriateWeight(confirmedCount, flaggedCount int) float64 {
	totalWeight := float64(confirmedCount) + (float64(flaggedCount) * 0.5)

	switch {
	case confirmedCount >= 5 || totalWeight >= 7:
		return 1.0
	case confirmedCount >= 4 || totalWeight >= 5:
		return 0.8
	case confirmedCount >= 3 || totalWeight >= 4:
		return 0.6
	case confirmedCount >= 2 || totalWeight >= 3:
		return 0.4
	case totalWeight >= 1:
		return 0.2
	default:
		return 0.0
	}
}

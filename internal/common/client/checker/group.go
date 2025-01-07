package checker

import (
	"context"
	"math"
	"sync"
	"time"

	apiTypes "github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/rotector/rotector/internal/common/client/fetcher"
	"github.com/rotector/rotector/internal/common/storage/database"
	"github.com/rotector/rotector/internal/common/storage/database/types"
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
	db                   *database.Client
	logger               *zap.Logger
	maxGroupMembersTrack uint64
	minFlaggedOverride   int
	minFlaggedPercentage float64
}

// NewGroupChecker creates a GroupChecker with database access for looking up
// flagged group information.
func NewGroupChecker(db *database.Client, logger *zap.Logger, maxGroupMembersTrack uint64, minFlaggedOverride int, minFlaggedPercentage float64) *GroupChecker {
	return &GroupChecker{
		db:                   db,
		logger:               logger,
		maxGroupMembersTrack: maxGroupMembersTrack,
		minFlaggedOverride:   minFlaggedOverride,
		minFlaggedPercentage: minFlaggedPercentage,
	}
}

// CheckGroupPercentages analyzes groups to find those exceeding the flagged user threshold.
func (c *GroupChecker) CheckGroupPercentages(groupInfos []*apiTypes.GroupResponse, groupToFlaggedUsers map[uint64][]uint64) []*types.FlaggedGroup {
	flaggedGroups := make([]*types.FlaggedGroup, 0)

	for _, groupInfo := range groupInfos {
		flaggedUsers := groupToFlaggedUsers[groupInfo.ID]

		// Skip if group has no flagged users
		if len(flaggedUsers) == 0 {
			continue
		}

		var reason string
		var confidence float64

		// Calculate percentage of flagged users
		percentage := (float64(len(flaggedUsers)) / float64(groupInfo.MemberCount)) * 100

		// Determine if and why the group should be flagged
		switch {
		case len(flaggedUsers) >= c.minFlaggedOverride:
			reason = "Group has large number of flagged users"
			confidence = math.Min(float64(len(flaggedUsers))/float64(c.minFlaggedOverride), 1.0)
		case percentage >= c.minFlaggedPercentage:
			reason = "Group has large percentage of flagged users"
			confidence = math.Min(percentage/c.minFlaggedPercentage, 1.0)
		default:
			continue
		}

		flaggedGroups = append(flaggedGroups, &types.FlaggedGroup{
			Group: types.Group{
				ID:          groupInfo.ID,
				Name:        groupInfo.Name,
				Description: groupInfo.Description,
				Owner:       groupInfo.Owner,
				Shout:       groupInfo.Shout,
				Reason:      reason,
				Confidence:  confidence,
				LastUpdated: time.Now(),
			},
		})
	}

	return flaggedGroups
}

// ProcessUsers checks multiple users' groups concurrently and returns flagged users.
func (c *GroupChecker) ProcessUsers(userInfos []*fetcher.Info) map[uint64]*types.User {
	// Collect all unique group IDs across all users
	uniqueGroupIDs := make(map[uint64]struct{})
	groupUsersTracking := make(map[uint64][]uint64)
	for _, userInfo := range userInfos {
		for _, group := range userInfo.Groups.Data {
			uniqueGroupIDs[group.Group.ID] = struct{}{}

			// Only track if member count is below threshold
			if group.Group.MemberCount <= c.maxGroupMembersTrack {
				groupUsersTracking[group.Group.ID] = append(groupUsersTracking[group.Group.ID], userInfo.ID)
			}
		}
	}

	// Convert unique IDs to slice
	groupIDs := make([]uint64, 0, len(uniqueGroupIDs))
	for groupID := range uniqueGroupIDs {
		groupIDs = append(groupIDs, groupID)
	}

	// Fetch all existing groups
	existingGroups, err := c.db.Groups().GetGroupsByIDs(context.Background(), groupIDs, types.GroupFields{
		Basic:  true,
		Reason: true,
	})
	if err != nil {
		c.logger.Error("Failed to fetch existing groups", zap.Error(err))
		return nil
	}

	// Track all users in groups
	if len(groupUsersTracking) > 0 {
		err = c.db.Tracking().AddUsersToGroupsTracking(context.Background(), groupUsersTracking)
		if err != nil {
			c.logger.Error("Failed to add users to groups tracking", zap.Error(err))
		}
	}

	// Process each user concurrently
	var wg sync.WaitGroup
	resultsChan := make(chan GroupCheckResult, len(userInfos))

	// Spawn a goroutine for each user
	for _, userInfo := range userInfos {
		wg.Add(1)
		go func(info *fetcher.Info) {
			defer wg.Done()

			// Process user groups
			user, autoFlagged := c.processUserGroups(info, existingGroups)
			resultsChan <- GroupCheckResult{
				UserID:      info.ID,
				User:        user,
				AutoFlagged: autoFlagged,
			}
		}(userInfo)
	}

	// Close channel when all goroutines complete
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// Collect flagged users
	flaggedUsers := make(map[uint64]*types.User)
	for result := range resultsChan {
		if result.AutoFlagged {
			flaggedUsers[result.UserID] = result.User
		}
	}

	return flaggedUsers
}

// processUserGroups checks if a user should be flagged based on their groups.
func (c *GroupChecker) processUserGroups(userInfo *fetcher.Info, existingGroups map[uint64]*types.ReviewGroup) (*types.User, bool) {
	// Skip users with very few groups to avoid false positives
	if len(userInfo.Groups.Data) < 2 {
		return nil, false
	}

	// Count confirmed and flagged groups
	confirmedCount := 0
	flaggedCount := 0

	for _, group := range userInfo.Groups.Data {
		if reviewGroup, exists := existingGroups[group.Group.ID]; exists {
			switch reviewGroup.Status {
			case types.GroupTypeConfirmed:
				confirmedCount++
			case types.GroupTypeFlagged:
				flaggedCount++
			} //exhaustive:ignore
		}
	}

	// Calculate confidence score
	confidence := c.calculateConfidence(confirmedCount, flaggedCount, len(userInfo.Groups.Data))

	// Flag user if confidence exceeds threshold
	if confidence >= 0.4 {
		c.logger.Info("User automatically flagged",
			zap.Uint64("userID", userInfo.ID),
			zap.Int("confirmedGroups", confirmedCount),
			zap.Int("flaggedGroups", flaggedCount),
			zap.Float64("confidence", confidence))

		return &types.User{
			ID:             userInfo.ID,
			Name:           userInfo.Name,
			DisplayName:    userInfo.DisplayName,
			Description:    userInfo.Description,
			CreatedAt:      userInfo.CreatedAt,
			Reason:         "Group Analysis: Member of multiple inappropriate groups.",
			Groups:         userInfo.Groups.Data,
			Friends:        userInfo.Friends.Data,
			Games:          userInfo.Games.Data,
			FollowerCount:  userInfo.FollowerCount,
			FollowingCount: userInfo.FollowingCount,
			Confidence:     math.Round(confidence*100) / 100, // Round to 2 decimal places
			LastUpdated:    userInfo.LastUpdated,
		}, true
	}

	return nil, false
}

// calculateConfidence computes a weighted confidence score based on group memberships.
func (c *GroupChecker) calculateConfidence(confirmedCount, flaggedCount, totalGroups int) float64 {
	var confidence float64

	// Factor 1: Absolute number of inappropriate groups - 60% weight
	inappropriateWeight := c.calculateInappropriateWeight(confirmedCount, flaggedCount)
	confidence += inappropriateWeight * 0.60

	// Factor 2: Ratio of inappropriate groups - 40% weight
	if totalGroups > 0 {
		totalInappropriate := float64(confirmedCount) + (float64(flaggedCount) * 0.5)
		ratioWeight := math.Min(totalInappropriate/float64(totalGroups), 1.0)
		confidence += ratioWeight * 0.40
	}

	return confidence
}

// calculateInappropriateWeight returns a weight based on the total number of inappropriate groups.
func (c *GroupChecker) calculateInappropriateWeight(confirmedCount, flaggedCount int) float64 {
	totalWeight := float64(confirmedCount) + (float64(flaggedCount) * 0.5)

	switch {
	case confirmedCount >= 4 || totalWeight >= 6:
		return 1.0
	case confirmedCount >= 3 || totalWeight >= 4:
		return 0.8
	case confirmedCount >= 2 || totalWeight >= 3:
		return 0.6
	case confirmedCount >= 1 || totalWeight >= 1:
		return 0.4
	default:
		return 0.0
	}
}

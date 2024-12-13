package checker

import (
	"context"
	"math"
	"sync"

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
	db     *database.Client
	logger *zap.Logger
}

// NewGroupChecker creates a GroupChecker with database access for looking up
// flagged group information.
func NewGroupChecker(db *database.Client, logger *zap.Logger) *GroupChecker {
	return &GroupChecker{
		db:     db,
		logger: logger,
	}
}

// ProcessUsers checks multiple users' groups concurrently and returns flagged users.
func (c *GroupChecker) ProcessUsers(userInfos []*fetcher.Info) map[uint64]*types.User {
	// Collect all unique group IDs across all users
	uniqueGroupIDs := make(map[uint64]struct{})
	groupToUsers := make(map[uint64][]uint64)
	for _, userInfo := range userInfos {
		for _, group := range userInfo.Groups.Data {
			uniqueGroupIDs[group.Group.ID] = struct{}{}
			groupToUsers[group.Group.ID] = append(groupToUsers[group.Group.ID], userInfo.ID)
		}
	}

	// Convert unique IDs to slice
	groupIDs := make([]uint64, 0, len(uniqueGroupIDs))
	for groupID := range uniqueGroupIDs {
		groupIDs = append(groupIDs, groupID)
	}

	// Fetch all existing groups
	existingGroups, groupTypes, err := c.db.Groups().GetGroupsByIDs(context.Background(), groupIDs, types.GroupFields{
		Basic:  true,
		Reason: true,
	})
	if err != nil {
		c.logger.Error("Failed to fetch existing groups", zap.Error(err))
		return nil
	}

	// Track all users in groups
	err = c.db.Tracking().AddUsersToGroupsTracking(context.Background(), groupToUsers)
	if err != nil {
		c.logger.Error("Failed to add users to groups tracking", zap.Error(err))
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
			user, autoFlagged := c.processUserGroups(info, existingGroups, groupTypes)
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
func (c *GroupChecker) processUserGroups(userInfo *fetcher.Info, existingGroups map[uint64]*types.Group, groupTypes map[uint64]types.GroupType) (*types.User, bool) {
	// Skip users with no group memberships
	if len(userInfo.Groups.Data) == 0 {
		return nil, false
	}

	// Count confirmed and flagged groups
	confirmedGroups := make(map[uint64]*types.Group)
	flaggedGroups := make(map[uint64]*types.Group)
	confirmedCount := 0
	flaggedCount := 0

	for _, group := range userInfo.Groups.Data {
		if existingGroup, exists := existingGroups[group.Group.ID]; exists {
			switch groupTypes[group.Group.ID] {
			case types.GroupTypeConfirmed:
				confirmedCount++
				confirmedGroups[group.Group.ID] = existingGroup
			case types.GroupTypeFlagged:
				flaggedCount++
				flaggedGroups[group.Group.ID] = existingGroup
			} //exhaustive:ignore
		}
	}

	// Calculate confidence score based on group memberships
	confidence := c.calculateConfidence(confirmedCount, flaggedCount, len(userInfo.Groups.Data))

	// Auto-flag users in 2 or more inappropriate groups
	if confidence >= 0.4 {
		user := &types.User{
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
		}

		c.logger.Info("User automatically flagged",
			zap.Uint64("userID", userInfo.ID),
			zap.Int("confirmedGroups", confirmedCount),
			zap.Int("flaggedGroups", flaggedCount),
			zap.Float64("confidence", confidence))

		return user, true
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

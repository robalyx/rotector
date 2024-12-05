package checker

import (
	"context"
	"fmt"
	"math"
	"sync"

	"github.com/rotector/rotector/internal/common/client/fetcher"
	"github.com/rotector/rotector/internal/common/storage/database"
	"github.com/rotector/rotector/internal/common/storage/database/types"
	"go.uber.org/zap"
)

// GroupCheckResult contains the result of checking a user's groups.
type GroupCheckResult struct {
	User        *types.User
	AutoFlagged bool
	Error       error
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

// ProcessUsers checks multiple users' groups concurrently and returns flagged users
// and remaining users that need further checking.
func (gc *GroupChecker) ProcessUsers(userInfos []*fetcher.Info) (map[uint64]*types.User, []*fetcher.Info) {
	// GroupCheckResult contains the result of checking a user's groups.
	type GroupCheckResult struct {
		UserID      uint64
		User        *types.User
		AutoFlagged bool
		Error       error
	}

	var wg sync.WaitGroup
	resultsChan := make(chan GroupCheckResult, len(userInfos))

	// Spawn a goroutine for each user
	for _, userInfo := range userInfos {
		wg.Add(1)
		go func(info *fetcher.Info) {
			defer wg.Done()

			// Process user groups
			user, autoFlagged, err := gc.processUserGroups(info)
			resultsChan <- GroupCheckResult{
				UserID:      info.ID,
				User:        user,
				AutoFlagged: autoFlagged,
				Error:       err,
			}
		}(userInfo)
	}

	// Close the channel when all goroutines are done
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// Collect user infos and results
	userInfoMap := make(map[uint64]*fetcher.Info)
	for _, info := range userInfos {
		userInfoMap[info.ID] = info
	}

	results := make(map[uint64]*GroupCheckResult)
	for result := range resultsChan {
		results[result.UserID] = &result
	}

	// Separate users into flagged and remaining
	flaggedUsers := make(map[uint64]*types.User)
	var remainingUsers []*fetcher.Info

	for userID, result := range results {
		if result.Error != nil {
			gc.logger.Error("Error checking user groups",
				zap.Error(result.Error),
				zap.Uint64("userID", userID))
			remainingUsers = append(remainingUsers, userInfoMap[userID])
			continue
		}

		if result.AutoFlagged {
			flaggedUsers[userID] = result.User
		} else {
			remainingUsers = append(remainingUsers, userInfoMap[userID])
		}
	}

	return flaggedUsers, remainingUsers
}

// processUserGroups checks if a user belongs to multiple flagged groups.
// The confidence score increases with the number of flagged groups relative
// to total group membership.
func (gc *GroupChecker) processUserGroups(userInfo *fetcher.Info) (*types.User, bool, error) {
	// Skip users with no group memberships
	if len(userInfo.Groups.Data) == 0 {
		return nil, false, nil
	}

	// Track user groups concurrently without blocking
	for _, group := range userInfo.Groups.Data {
		go func(groupID, userID uint64) {
			err := gc.db.Tracking().AddUserToGroupTracking(context.Background(), groupID, userID)
			if err != nil {
				gc.logger.Error("Failed to add user to group tracking",
					zap.Error(err),
					zap.Uint64("groupID", groupID),
					zap.Uint64("userID", userID))
			}
		}(group.Group.ID, userInfo.ID)
	}

	// Extract group IDs for batch lookup
	groupIDs := make([]uint64, len(userInfo.Groups.Data))
	for i, group := range userInfo.Groups.Data {
		groupIDs[i] = group.Group.ID
	}

	// Check database for groups in all tables
	existingGroups, groupTypes, err := gc.db.Groups().GetGroupsByIDs(context.Background(), groupIDs, types.GroupFields{
		Basic:  true,
		Reason: true,
	})
	if err != nil {
		return nil, false, err
	}

	// Separate groups by type and count them
	confirmedGroups := make(map[uint64]*types.Group)
	flaggedGroups := make(map[uint64]*types.Group)
	confirmedCount := 0
	flaggedCount := 0

	for id, group := range existingGroups {
		switch groupTypes[id] {
		case types.GroupTypeConfirmed:
			confirmedCount++
			confirmedGroups[id] = group
		case types.GroupTypeFlagged:
			flaggedCount++
			flaggedGroups[id] = group
		} //exhaustive:ignore
	}

	// Calculate confidence score based on group memberships
	confidence := gc.calculateConfidence(confirmedCount, flaggedCount, len(userInfo.Groups.Data))

	// Auto-flag users in 2 or more inappropriate groups
	if confidence >= 0.4 {
		// Generate reason based on group memberships
		reason := fmt.Sprintf(
			"Member of %d confirmed and %d flagged inappropriate groups (%.1f%% total).",
			confirmedCount,
			flaggedCount,
			float64(confirmedCount+flaggedCount)/float64(len(userInfo.Groups.Data))*100,
		)

		user := &types.User{
			ID:             userInfo.ID,
			Name:           userInfo.Name,
			DisplayName:    userInfo.DisplayName,
			Description:    userInfo.Description,
			CreatedAt:      userInfo.CreatedAt,
			Reason:         "Group Analysis: " + reason,
			Groups:         userInfo.Groups.Data,
			Friends:        userInfo.Friends.Data,
			Games:          userInfo.Games.Data,
			FollowerCount:  userInfo.FollowerCount,
			FollowingCount: userInfo.FollowingCount,
			FlaggedGroups:  gc.getGroupIDs(confirmedGroups, flaggedGroups),
			Confidence:     math.Round(confidence*100) / 100, // Round to 2 decimal places
			LastUpdated:    userInfo.LastUpdated,
		}

		gc.logger.Info("User automatically flagged",
			zap.Uint64("userID", userInfo.ID),
			zap.Int("confirmedGroups", confirmedCount),
			zap.Int("flaggedGroups", flaggedCount),
			zap.Float64("confidence", confidence))

		return user, true, nil
	}

	return nil, false, nil
}

// calculateConfidence computes a weighted confidence score based on group memberships.
func (gc *GroupChecker) calculateConfidence(confirmedCount, flaggedCount, totalGroups int) float64 {
	var confidence float64

	// Factor 1: Absolute number of inappropriate groups - 60% weight
	inappropriateWeight := gc.calculateInappropriateWeight(confirmedCount, flaggedCount)
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
func (gc *GroupChecker) calculateInappropriateWeight(confirmedCount, flaggedCount int) float64 {
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

// getGroupIDs combines IDs from confirmed and flagged groups.
func (gc *GroupChecker) getGroupIDs(confirmedGroups, flaggedGroups map[uint64]*types.Group) []uint64 {
	flaggedIDs := make([]uint64, 0, len(confirmedGroups)+len(flaggedGroups))

	for id := range confirmedGroups {
		flaggedIDs = append(flaggedIDs, id)
	}
	for id := range flaggedGroups {
		flaggedIDs = append(flaggedIDs, id)
	}

	return flaggedIDs
}

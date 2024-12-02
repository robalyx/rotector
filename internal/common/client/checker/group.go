package checker

import (
	"context"
	"fmt"
	"sync"

	"github.com/rotector/rotector/internal/common/client/fetcher"
	"github.com/rotector/rotector/internal/common/storage/database"
	"github.com/rotector/rotector/internal/common/storage/database/models"
	"go.uber.org/zap"
)

// GroupCheckResult contains the result of checking a user's groups.
type GroupCheckResult struct {
	User        *models.User
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
func (gc *GroupChecker) ProcessUsers(userInfos []*fetcher.Info) (map[uint64]*models.User, []*fetcher.Info) {
	// GroupCheckResult contains the result of checking a user's groups.
	type GroupCheckResult struct {
		UserID      uint64
		User        *models.User
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
	flaggedUsers := make(map[uint64]*models.User)
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
func (gc *GroupChecker) processUserGroups(userInfo *fetcher.Info) (*models.User, bool, error) {
	// Skip users with no group memberships
	if len(userInfo.Groups.Data) == 0 {
		return nil, false, nil
	}

	// Track user groups concurrently
	gc.trackUserGroups(userInfo)

	// Extract group IDs for batch lookup
	groupIDs := make([]uint64, len(userInfo.Groups.Data))
	for i, group := range userInfo.Groups.Data {
		groupIDs[i] = group.Group.ID
	}

	// Check database for flagged groups
	flaggedGroupIDs, err := gc.db.Groups().CheckConfirmedGroups(context.Background(), groupIDs)
	if err != nil {
		return nil, false, err
	}

	// Auto-flag users in 2 or more flagged groups
	if len(flaggedGroupIDs) >= 2 {
		user := &models.User{
			ID:             userInfo.ID,
			Name:           userInfo.Name,
			DisplayName:    userInfo.DisplayName,
			Description:    userInfo.Description,
			CreatedAt:      userInfo.CreatedAt,
			Reason:         fmt.Sprintf("Group Analysis: Member of %d flagged groups", len(flaggedGroupIDs)),
			Groups:         userInfo.Groups.Data,
			Friends:        userInfo.Friends.Data,
			Games:          userInfo.Games.Data,
			FollowerCount:  userInfo.FollowerCount,
			FollowingCount: userInfo.FollowingCount,
			FlaggedGroups:  flaggedGroupIDs,
			Confidence:     float64(len(flaggedGroupIDs)) / float64(len(userInfo.Groups.Data)),
			LastUpdated:    userInfo.LastUpdated,
		}

		gc.logger.Info("User automatically flagged",
			zap.Uint64("userID", userInfo.ID),
			zap.Uint64s("flaggedGroupIDs", flaggedGroupIDs))

		return user, true, nil
	}

	return nil, false, nil
}

// trackUserGroups handles concurrent tracking of user's group memberships.
func (gc *GroupChecker) trackUserGroups(userInfo *fetcher.Info) {
	type TrackingError struct {
		GroupID uint64
		Err     error
	}

	var wg sync.WaitGroup
	trackingErrChan := make(chan TrackingError, len(userInfo.Groups.Data))

	for _, group := range userInfo.Groups.Data {
		wg.Add(1)
		go func(groupID, userID uint64) {
			defer wg.Done()

			err := gc.db.Tracking().AddUserToGroupTracking(context.Background(), groupID, userID)
			if err != nil {
				trackingErrChan <- TrackingError{
					GroupID: groupID,
					Err:     err,
				}
			}
		}(group.Group.ID, userInfo.ID)
	}

	// Close error channel when all goroutines complete
	go func() {
		wg.Wait()
		close(trackingErrChan)
	}()

	// Log all errors
	for err := range trackingErrChan {
		gc.logger.Error("Failed to add user to group tracking",
			zap.Error(err.Err),
			zap.Uint64("userID", userInfo.ID),
			zap.Uint64("groupID", err.GroupID))
	}
}

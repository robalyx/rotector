package checker

import (
	"context"
	"fmt"
	"sync"

	"github.com/rotector/rotector/internal/common/database"
	"github.com/rotector/rotector/internal/common/fetcher"
	"go.uber.org/zap"
)

// GroupCheckResult contains the result of checking a user's groups.
type GroupCheckResult struct {
	User        *database.User
	AutoFlagged bool
	Error       error
}

// GroupChecker handles the checking of user groups by comparing them against
// a database of known inappropriate groups.
type GroupChecker struct {
	db     *database.Database
	logger *zap.Logger
}

// NewGroupChecker creates a GroupChecker with database access for looking up
// flagged group information.
func NewGroupChecker(db *database.Database, logger *zap.Logger) *GroupChecker {
	return &GroupChecker{
		db:     db,
		logger: logger,
	}
}

// ProcessUsers checks multiple users' groups concurrently and returns flagged users
// and remaining users that need further checking.
func (gc *GroupChecker) ProcessUsers(userInfos []*fetcher.Info) ([]*database.User, []*fetcher.Info) {
	// GroupCheckResult contains the result of checking a user's groups.
	type GroupCheckResult struct {
		UserID      uint64
		User        *database.User
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
	var flaggedUsers []*database.User
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
			flaggedUsers = append(flaggedUsers, result.User)
		} else {
			remainingUsers = append(remainingUsers, userInfoMap[userID])
		}
	}

	return flaggedUsers, remainingUsers
}

// processUserGroups checks if a user belongs to multiple flagged groups.
// The confidence score increases with the number of flagged groups relative
// to total group membership.
func (gc *GroupChecker) processUserGroups(userInfo *fetcher.Info) (*database.User, bool, error) {
	// Skip users with no group memberships
	if len(userInfo.Groups) == 0 {
		return nil, false, nil
	}

	// Extract group IDs for batch lookup
	groupIDs := make([]uint64, len(userInfo.Groups))
	for i, group := range userInfo.Groups {
		groupIDs[i] = group.Group.ID
	}

	// Check database for flagged groups
	flaggedGroupIDs, err := gc.db.Groups().CheckConfirmedGroups(context.Background(), groupIDs)
	if err != nil {
		return nil, false, err
	}

	// Auto-flag users in 2 or more flagged groups
	if len(flaggedGroupIDs) >= 2 {
		user := &database.User{
			ID:            userInfo.ID,
			Name:          userInfo.Name,
			DisplayName:   userInfo.DisplayName,
			Description:   userInfo.Description,
			CreatedAt:     userInfo.CreatedAt,
			Reason:        fmt.Sprintf("Member of %d flagged groups", len(flaggedGroupIDs)),
			Groups:        userInfo.Groups,
			FlaggedGroups: flaggedGroupIDs,
			Confidence:    float64(len(flaggedGroupIDs)) / float64(len(userInfo.Groups)),
			LastUpdated:   userInfo.LastUpdated,
		}

		gc.logger.Info("User automatically flagged",
			zap.Uint64("userID", userInfo.ID),
			zap.Uint64s("flaggedGroupIDs", flaggedGroupIDs))

		return user, true, nil
	}

	return nil, false, nil
}

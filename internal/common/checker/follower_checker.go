package checker

import (
	"context"
	"fmt"
	"sync"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/rotector/rotector/internal/common/database"
	"go.uber.org/zap"
)

const (
	// FollowerThreshold is the number of followers that indicates a popular user.
	FollowerThreshold = 1000

	// WarningTemplate is the template for the warning message.
	WarningTemplate = "⚠️ **WARNING: Popular user with %d followers** ⚠️\n\n%s"
)

// FollowerChecker handles checking if flagged users are popular based on follower count.
type FollowerChecker struct {
	roAPI  *api.API
	logger *zap.Logger
}

// NewFollowerChecker creates a new FollowerChecker.
func NewFollowerChecker(roAPI *api.API, logger *zap.Logger) *FollowerChecker {
	return &FollowerChecker{
		roAPI:  roAPI,
		logger: logger,
	}
}

// ProcessUsers checks follower counts for flagged users and updates their confidence/reason if popular.
func (fc *FollowerChecker) ProcessUsers(flaggedUsers []*database.User) []*database.User {
	// FollowerCheckResult contains the result of checking a user's followers.
	type FollowerCheckResult struct {
		UserID    uint64
		Followers uint64
		Error     error
	}

	var wg sync.WaitGroup
	resultsChan := make(chan FollowerCheckResult, len(flaggedUsers))

	// Check followers for each user concurrently
	for _, user := range flaggedUsers {
		wg.Add(1)
		go func(userID uint64) {
			defer wg.Done()

			// Get follower count from API
			followers, err := fc.roAPI.Friends().GetFollowerCount(context.Background(), userID)
			resultsChan <- FollowerCheckResult{
				UserID:    userID,
				Followers: followers,
				Error:     err,
			}
		}(user.ID)
	}

	// Close channel when all checks are complete
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// Collect results from the channel
	results := make(map[uint64]uint64)
	for result := range resultsChan {
		if result.Error != nil {
			fc.logger.Error("Error getting follower count",
				zap.Error(result.Error),
				zap.Uint64("userID", result.UserID))
			continue
		}
		results[result.UserID] = result.Followers
	}

	// Update confidence and reason for popular users
	updatedUsers := make([]*database.User, len(flaggedUsers))
	for i, user := range flaggedUsers {
		followers := results[user.ID]
		if followers >= FollowerThreshold {
			user.Confidence = 1.0
			user.Reason = fmt.Sprintf(WarningTemplate, followers, user.Reason)

			fc.logger.Info("Popular user flagged",
				zap.Uint64("userID", user.ID),
				zap.String("username", user.Name),
				zap.Uint64("followers", followers))
		}

		updatedUsers[i] = user
	}

	return updatedUsers
}

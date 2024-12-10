package fetcher

import (
	"context"
	"errors"
	"sync"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/rotector/rotector/internal/common/storage/database/types"
	"go.uber.org/zap"
)

// FollowFetchResult contains the follower/following counts.
type FollowFetchResult struct {
	ID             uint64
	FollowerCount  uint64
	FollowingCount uint64
	Error          error
}

// FollowFetcher handles retrieval of user follow counts from the Roblox API.
type FollowFetcher struct {
	roAPI  *api.API
	logger *zap.Logger
}

// NewFollowFetcher creates a FollowFetcher with the provided API client and logger.
func NewFollowFetcher(roAPI *api.API, logger *zap.Logger) *FollowFetcher {
	return &FollowFetcher{
		roAPI:  roAPI,
		logger: logger,
	}
}

// AddFollowCounts fetches follow counts and returns a map of results.
func (f *FollowFetcher) AddFollowCounts(users map[uint64]*types.User) map[uint64]*FollowFetchResult {
	var wg sync.WaitGroup
	resultsChan := make(chan FollowFetchResult, len(users))

	// Process each user concurrently
	for _, user := range users {
		wg.Add(1)
		go func(u *types.User) {
			defer wg.Done()

			// Get follower and following counts
			followerCount, followerErr := f.roAPI.Friends().GetFollowerCount(context.Background(), u.ID)
			followingCount, followingErr := f.roAPI.Friends().GetFollowingCount(context.Background(), u.ID)

			// Send results
			resultsChan <- FollowFetchResult{
				ID:             u.ID,
				FollowerCount:  followerCount,
				FollowingCount: followingCount,
				Error:          errors.Join(followerErr, followingErr),
			}
		}(user)
	}

	// Close channel when all goroutines complete
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// Collect results from the channel
	results := make(map[uint64]*FollowFetchResult, len(users))
	for result := range resultsChan {
		if result.Error != nil {
			f.logger.Error("Failed to fetch follow counts",
				zap.Error(result.Error),
				zap.Uint64("userID", result.ID))
			continue
		}
		results[result.ID] = &result
	}

	f.logger.Debug("Finished fetching follow counts",
		zap.Int("totalUsers", len(users)),
		zap.Int("successfulFetches", len(results)))

	return results
}

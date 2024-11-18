package fetcher

import (
	"context"
	"sync"

	"github.com/jaxron/roapi.go/pkg/api"
	"go.uber.org/zap"
)

// FollowCounts contains the counts of followers and followings for a user.
type FollowCounts struct {
	FollowerCount  uint64
	FollowingCount uint64
}

// FollowFetcher handles retrieval of user follow information from the Roblox API.
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

// FetchUserFollowCounts retrieves follower and following counts for a single user concurrently.
func (f *FollowFetcher) FetchUserFollowCounts(userID uint64) (*FollowCounts, error) {
	var wg sync.WaitGroup
	var followerCount, followingCount uint64
	var followerErr, followingErr error

	// Fetch follower count
	wg.Add(1)
	go func() {
		defer wg.Done()
		count, err := f.roAPI.Friends().GetFollowerCount(context.Background(), userID)
		if err != nil {
			followerErr = err
			return
		}
		followerCount = count
	}()

	// Fetch following count
	wg.Add(1)
	go func() {
		defer wg.Done()
		count, err := f.roAPI.Friends().GetFollowingCount(context.Background(), userID)
		if err != nil {
			followingErr = err
			return
		}
		followingCount = count
	}()

	// Wait for both goroutines to complete
	wg.Wait()

	// Check for errors
	if followerErr != nil {
		return nil, followerErr
	}
	if followingErr != nil {
		return nil, followingErr
	}

	return &FollowCounts{
		FollowerCount:  followerCount,
		FollowingCount: followingCount,
	}, nil
}

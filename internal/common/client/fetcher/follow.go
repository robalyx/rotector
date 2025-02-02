package fetcher

import (
	"context"
	"errors"
	"sync"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/jaxron/roapi.go/pkg/api/middleware/auth"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/sourcegraph/conc/pool"
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

// AddFollowCounts fetches follow counts to a map of users.
func (f *FollowFetcher) AddFollowCounts(ctx context.Context, users map[uint64]*types.User) {
	ctx = context.WithValue(ctx, auth.KeyAddCookie, true)

	var (
		p  = pool.New().WithContext(ctx)
		mu sync.Mutex
	)

	// Process each user concurrently
	for _, user := range users {
		p.Go(func(ctx context.Context) error {
			// Get follower and following counts
			followerCount, followerErr := f.roAPI.Friends().GetFollowerCount(ctx, user.ID)
			followingCount, followingErr := f.roAPI.Friends().GetFollowingCount(ctx, user.ID)

			err := errors.Join(followerErr, followingErr)
			if err != nil {
				f.logger.Error("Failed to fetch follow counts",
					zap.Error(err),
					zap.Uint64("userID", user.ID))
				return nil // Don't fail the whole batch for one error
			}

			mu.Lock()
			users[user.ID].FollowerCount = followerCount
			users[user.ID].FollowingCount = followingCount
			mu.Unlock()

			return nil
		})
	}

	// Wait for all goroutines to complete
	if err := p.Wait(); err != nil {
		f.logger.Error("Error during follow counts fetch", zap.Error(err))
	}

	f.logger.Debug("Finished fetching follow counts",
		zap.Int("totalUsers", len(users)))
}

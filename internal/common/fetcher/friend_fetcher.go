package fetcher

import (
	"context"
	"sync"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/rotector/rotector/internal/common/database"
	"go.uber.org/zap"
)

// FriendFetcher handles fetching of user friends.
type FriendFetcher struct {
	roAPI  *api.API
	logger *zap.Logger
}

// NewFriendFetcher creates a new FriendFetcher instance.
func NewFriendFetcher(roAPI *api.API, logger *zap.Logger) *FriendFetcher {
	return &FriendFetcher{
		roAPI:  roAPI,
		logger: logger,
	}
}

// AddFriends fetches friends for a batch of users and adds them to the users.
func (f *FriendFetcher) AddFriends(users []*database.User) []*database.User {
	var wg sync.WaitGroup
	var mu sync.Mutex
	semaphore := make(chan struct{}, 100) // Limit concurrent requests to 100

	for i, user := range users {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			semaphore <- struct{}{}        // Acquire semaphore
			defer func() { <-semaphore }() // Release semaphore

			// Fetch friends for the user
			friends, err := f.roAPI.Friends().GetFriends(context.Background(), user.ID)
			if err != nil {
				f.logger.Error("Failed to fetch user friends", zap.Error(err), zap.Uint64("userID", user.ID))
				return
			}

			// Store the complete friend data
			mu.Lock()
			users[i].Friends = friends
			mu.Unlock()
		}(i)
	}

	wg.Wait()

	f.logger.Info("Finished fetching user friends", zap.Int("totalUsers", len(users)))
	return users
}

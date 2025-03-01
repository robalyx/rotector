package fetcher

import (
	"context"
	"fmt"
	"sync"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/jaxron/roapi.go/pkg/api/resources/friends"
	"github.com/jaxron/roapi.go/pkg/api/resources/users"
	apiTypes "github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/sourcegraph/conc/pool"
	"go.uber.org/zap"
)

// FriendFetcher handles retrieval of user friend information from the Roblox API.
type FriendFetcher struct {
	roAPI  *api.API
	logger *zap.Logger
}

// NewFriendFetcher creates a FriendFetcher with the provided API client and logger.
func NewFriendFetcher(roAPI *api.API, logger *zap.Logger) *FriendFetcher {
	return &FriendFetcher{
		roAPI:  roAPI,
		logger: logger,
	}
}

// GetFriendIDs returns the friend IDs for a user.
func (f *FriendFetcher) GetFriendIDs(ctx context.Context, userID uint64) ([]uint64, error) {
	// Get the friend count to determine which endpoint to use
	friendCount, err := f.roAPI.Friends().GetFriendCount(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get friend count: %w", err)
	}

	// For users with ≤200 friends, use the legacy endpoint and extract IDs
	if friendCount <= 200 {
		friends, err := f.getFriendsLegacy(ctx, userID)
		if err != nil {
			return nil, err
		}

		friendIDs := make([]uint64, 0, len(friends))
		for _, friend := range friends {
			friendIDs = append(friendIDs, friend.ID)
		}
		return friendIDs, nil
	}

	// For users with >200 friends, use pagination to collect IDs
	var (
		friendIDs []uint64
		cursor    string
	)

	for {
		// Create request builder
		builder := friends.NewFindFriendsBuilder(userID).
			WithLimit(50)

		if cursor != "" {
			builder.WithCursor(cursor)
		}

		// Fetch page of friends
		response, err := f.roAPI.Friends().FindFriends(ctx, builder.Build())
		if err != nil {
			return nil, fmt.Errorf("failed to get friends: %w", err)
		}

		// Add friend IDs to slice
		for _, friend := range response.PageItems {
			friendIDs = append(friendIDs, friend.ID)
		}

		// Check if there are more pages
		if response.NextCursor == nil {
			break
		}
		cursor = *response.NextCursor
	}

	return friendIDs, nil
}

// GetFriends returns a user's friends with full details using the best method.
func (f *FriendFetcher) GetFriends(ctx context.Context, userID uint64) ([]*apiTypes.ExtendedFriend, error) {
	// Get the friend count to determine which endpoint to use
	friendCount, err := f.roAPI.Friends().GetFriendCount(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get friend count: %w", err)
	}

	// For users with ≤200 friends, use the legacy endpoint which includes all details
	if friendCount <= 200 {
		return f.getFriendsLegacy(ctx, userID)
	}

	// For users with >200 friends, get IDs then fetch details
	friendIDs, err := f.GetFriendIDs(ctx, userID)
	if err != nil {
		return nil, err
	}

	var (
		allFriends = make([]*apiTypes.ExtendedFriend, 0, len(friendIDs))
		p          = pool.New().WithContext(ctx)
		mu         sync.Mutex
		batchSize  = 100
	)

	// Process batches concurrently
	for i := 0; i < len(friendIDs); i += batchSize {
		end := min(i+batchSize, len(friendIDs))

		batchIDs := friendIDs[i:end]
		p.Go(func(ctx context.Context) error {
			builder := users.NewUsersByIDsBuilder(batchIDs...)
			userDetails, err := f.roAPI.Users().GetUsersByIDs(ctx, builder.Build())
			if err != nil {
				f.logger.Error("Failed to fetch user details",
					zap.Error(err),
					zap.Int("batchStart", i),
					zap.Int("batchEnd", end))
				return nil // Don't fail the whole batch for one error
			}

			batchFriends := make([]*apiTypes.ExtendedFriend, 0, len(userDetails.Data))
			for _, user := range userDetails.Data {
				batchFriends = append(batchFriends, &apiTypes.ExtendedFriend{
					Friend: apiTypes.Friend{
						ID: user.ID,
					},
					Name:        user.Name,
					DisplayName: user.DisplayName,
				})
			}

			mu.Lock()
			allFriends = append(allFriends, batchFriends...)
			mu.Unlock()

			return nil
		})
	}

	// Wait for all goroutines to complete
	if err := p.Wait(); err != nil {
		f.logger.Error("Error during friend details fetch", zap.Error(err))
		return nil, err
	}

	f.logger.Debug("Finished fetching friends using pagination",
		zap.Uint64("userID", userID),
		zap.Int("totalFriends", len(friendIDs)),
		zap.Int("successfulFetches", len(allFriends)))

	return allFriends, nil
}

// getFriendsLegacy returns a user's friends with full details using the legacy endpoint.
func (f *FriendFetcher) getFriendsLegacy(ctx context.Context, userID uint64) ([]*apiTypes.ExtendedFriend, error) {
	response, err := f.roAPI.Friends().GetFriends(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get friends: %w", err)
	}

	friends := make([]*apiTypes.ExtendedFriend, 0, len(response.Data))
	for _, friend := range response.Data {
		friends = append(friends, &friend)
	}

	f.logger.Debug("Finished fetching friends using legacy endpoint",
		zap.Uint64("userID", userID),
		zap.Int("totalFriends", len(friends)))

	return friends, nil
}

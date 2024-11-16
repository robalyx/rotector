package fetcher

import (
	"context"
	"sync"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/jaxron/roapi.go/pkg/api/resources/friends"
	"github.com/jaxron/roapi.go/pkg/api/resources/users"
	"github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/rotector/rotector/internal/common/database"
	"go.uber.org/zap"
)

// FriendFetchResult contains the result of fetching a user's friends.
type FriendFetchResult struct {
	Friends []database.ExtendedFriend
	Error   error
}

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

// GetFriends retrieves all friends for a user ID, handling pagination and user details.
func (f *FriendFetcher) GetFriends(ctx context.Context, userID uint64) ([]database.ExtendedFriend, error) {
	// First, get all friend IDs using pagination
	friendIDs, err := f.getAllFriendIDs(ctx, userID)
	if err != nil {
		return nil, err
	}

	if len(friendIDs) == 0 {
		return []database.ExtendedFriend{}, nil
	}

	// Then fetch user details in batches
	return f.getFriendDetails(ctx, friendIDs)
}

// getAllFriendIDs retrieves all friend IDs using the new paginated endpoint.
func (f *FriendFetcher) getAllFriendIDs(ctx context.Context, userID uint64) ([]uint64, error) {
	var friendIDs []uint64
	var cursor string

	for {
		// Create request builder
		builder := friends.NewFindFriendsBuilder(userID).
			WithLimit(50) // Max limit per page

		if cursor != "" {
			builder.WithCursor(cursor)
		}

		// Fetch page of friends
		response, err := f.roAPI.Friends().FindFriends(ctx, builder.Build())
		if err != nil {
			return nil, err
		}

		// Extract IDs from page items
		for _, friend := range response.PageItems {
			friendIDs = append(friendIDs, friend.ID)
		}

		// Check if there are more pages
		if !response.HasMore || response.NextCursor == nil {
			break
		}

		cursor = *response.NextCursor
	}

	return friendIDs, nil
}

// getFriendDetails fetches user details for friend IDs in batches concurrently.
func (f *FriendFetcher) getFriendDetails(ctx context.Context, friendIDs []uint64) ([]database.ExtendedFriend, error) {
	type batchResult struct {
		friends []database.ExtendedFriend
		err     error
	}

	var wg sync.WaitGroup
	batchSize := 100                                           // Max users per request
	numBatches := (len(friendIDs) + batchSize - 1) / batchSize // Ceiling division
	resultsChan := make(chan batchResult, numBatches)

	// Process batches concurrently
	for i := 0; i < len(friendIDs); i += batchSize {
		wg.Add(1)
		go func(start int) {
			defer wg.Done()

			end := start + batchSize
			if end > len(friendIDs) {
				end = len(friendIDs)
			}

			// Create batch request
			builder := users.NewUsersByIDsBuilder(friendIDs[start:end]...)
			userDetails, err := f.roAPI.Users().GetUsersByIDs(ctx, builder.Build())
			if err != nil {
				resultsChan <- batchResult{err: err}
				return
			}

			// Convert user details to ExtendedFriend type
			batchFriends := make([]database.ExtendedFriend, 0, len(userDetails))
			for _, user := range userDetails {
				batchFriends = append(batchFriends, database.ExtendedFriend{
					Friend: types.Friend{
						ID: user.ID,
					},
					Name:             user.Name,
					DisplayName:      user.DisplayName,
					HasVerifiedBadge: user.HasVerifiedBadge,
				})
			}

			resultsChan <- batchResult{friends: batchFriends}
		}(i)
	}

	// Close channel when all goroutines complete
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// Collect results from all batches
	var allFriends []database.ExtendedFriend
	for result := range resultsChan {
		if result.err != nil {
			return nil, result.err // Return on first error
		}
		allFriends = append(allFriends, result.friends...)
	}

	return allFriends, nil
}

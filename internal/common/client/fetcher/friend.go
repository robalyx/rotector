package fetcher

import (
	"context"
	"sync"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/jaxron/roapi.go/pkg/api/resources/friends"
	"github.com/jaxron/roapi.go/pkg/api/resources/users"
	"github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/rotector/rotector/internal/common/storage/database/models"
	"go.uber.org/zap"
)

// FriendFetchResult contains the result of fetching a user's friends.
type FriendFetchResult struct {
	Data  []models.ExtendedFriend
	Error error
}

// FriendDetails contains the result of fetching a user's friends.
type FriendDetails struct {
	Data []models.ExtendedFriend
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
func (f *FriendFetcher) GetFriends(ctx context.Context, userID uint64) ([]models.ExtendedFriend, error) {
	// First, get all friend IDs using pagination
	friendIDs, err := f.getAllFriendIDs(ctx, userID)
	if err != nil {
		return nil, err
	}

	// If no friends, return an empty slice
	if len(friendIDs) == 0 {
		return []models.ExtendedFriend{}, nil
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
func (f *FriendFetcher) getFriendDetails(ctx context.Context, friendIDs []uint64) ([]models.ExtendedFriend, error) {
	var wg sync.WaitGroup
	batchSize := 100
	numBatches := (len(friendIDs) + batchSize - 1) / batchSize
	resultsChan := make(chan FriendFetchResult, numBatches)

	// Process batches concurrently
	for i := 0; i < len(friendIDs); i += batchSize {
		wg.Add(1)
		go func(start int) {
			defer wg.Done()

			// Get the current batch of friend IDs
			end := start + batchSize
			if end > len(friendIDs) {
				end = len(friendIDs)
			}
			batchIDs := friendIDs[start:end]

			// Fetch user details for the batch
			builder := users.NewUsersByIDsBuilder(batchIDs...)
			userDetails, err := f.roAPI.Users().GetUsersByIDs(ctx, builder.Build())
			if err != nil {
				resultsChan <- FriendFetchResult{Error: err}
				return
			}

			// Convert user details to ExtendedFriend type
			batchFriends := make([]models.ExtendedFriend, 0, len(userDetails.Data))
			for _, user := range userDetails.Data {
				batchFriends = append(batchFriends, models.ExtendedFriend{
					Friend: types.Friend{
						ID: user.ID,
					},
					Name:             user.Name,
					DisplayName:      user.DisplayName,
					HasVerifiedBadge: user.HasVerifiedBadge,
				})
			}

			resultsChan <- FriendFetchResult{Data: batchFriends}
		}(i)
	}

	// Close channel when all goroutines complete
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// Pre-allocate the slice with estimated capacity
	allFriends := make([]models.ExtendedFriend, 0, len(friendIDs))

	// Collect results from all batches
	for result := range resultsChan {
		if result.Error != nil {
			return nil, result.Error
		}

		allFriends = append(allFriends, result.Data...)
	}

	f.logger.Debug("Finished fetching friend details",
		zap.Int("totalRequested", len(friendIDs)),
		zap.Int("successfulFetches", len(allFriends)))

	return allFriends, nil
}

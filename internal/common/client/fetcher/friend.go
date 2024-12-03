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
	var allFriends []models.ExtendedFriend
	var cursor string
	batchSize := 100 // Process friend details in batches of 100
	currentBatch := make([]uint64, 0, batchSize)

	// Channel to collect results from batch processing goroutines
	resultsChan := make(chan FriendFetchResult)
	var wg sync.WaitGroup

	// Start a goroutine to wait for all batches and close channel
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

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

		// Add friend IDs to current batch
		for _, friend := range response.PageItems {
			currentBatch = append(currentBatch, friend.ID)

			// Process batch when it reaches batchSize
			if len(currentBatch) >= batchSize {
				wg.Add(1)
				go func(batch []uint64) {
					defer wg.Done()
					friendDetails, err := f.getFriendDetails(ctx, batch)
					resultsChan <- FriendFetchResult{
						Data:  friendDetails,
						Error: err,
					}
				}(currentBatch)

				currentBatch = make([]uint64, 0, batchSize)
			}
		}

		// Check if there are more pages
		if !response.HasMore || response.NextCursor == nil {
			break
		}

		cursor = *response.NextCursor
	}

	// Process any remaining friends in the last batch
	if len(currentBatch) > 0 {
		friendDetails, err := f.getFriendDetails(ctx, currentBatch)
		if err != nil {
			f.logger.Error("Error processing final batch", zap.Error(err))
		} else {
			allFriends = append(allFriends, friendDetails...)
		}
	}

	// Collect results from all batches
	for result := range resultsChan {
		if result.Error != nil {
			f.logger.Error("Error processing friend batch", zap.Error(result.Error))
			continue
		}
		allFriends = append(allFriends, result.Data...)
	}

	return allFriends, nil
}

// getFriendDetails fetches user details for a batch of friend IDs.
func (f *FriendFetcher) getFriendDetails(ctx context.Context, friendIDs []uint64) ([]models.ExtendedFriend, error) {
	// Fetch user details for the batch
	builder := users.NewUsersByIDsBuilder(friendIDs...)
	userDetails, err := f.roAPI.Users().GetUsersByIDs(ctx, builder.Build())
	if err != nil {
		return nil, err
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

	f.logger.Debug("Finished fetching friend details",
		zap.Int("totalRequested", len(friendIDs)),
		zap.Int("successfulFetches", len(batchFriends)))

	return batchFriends, nil
}

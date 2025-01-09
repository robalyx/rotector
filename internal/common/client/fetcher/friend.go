package fetcher

import (
	"context"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/jaxron/roapi.go/pkg/api/resources/friends"
	"github.com/jaxron/roapi.go/pkg/api/resources/users"
	apiTypes "github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"go.uber.org/zap"
)

// FriendFetchResult contains the result of fetching a user's friends.
type FriendFetchResult struct {
	Data  []types.ExtendedFriend
	Error error
}

// FriendDetails contains the result of fetching a user's friends.
type FriendDetails struct {
	Data []types.ExtendedFriend
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

// GetFriends retrieves the friend IDs for a user with pagination handling.
func (f *FriendFetcher) GetFriends(ctx context.Context, userID uint64) ([]uint64, error) {
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

// GetFriendsWithDetails retrieves all friends with their extended details for a user.
func (f *FriendFetcher) GetFriendsWithDetails(ctx context.Context, userID uint64) ([]types.ExtendedFriend, error) {
	// Get all friend IDs
	friendIDs, err := f.GetFriends(ctx, userID)
	if err != nil {
		return nil, err
	}

	var allFriends []types.ExtendedFriend
	batchSize := 100

	// Process friendIDs in batches
	for i := 0; i < len(friendIDs); i += batchSize {
		end := i + batchSize
		if end > len(friendIDs) {
			end = len(friendIDs)
		}

		// Fetch user details for the current batch
		builder := users.NewUsersByIDsBuilder(friendIDs[i:end]...)
		userDetails, err := f.roAPI.Users().GetUsersByIDs(ctx, builder.Build())
		if err != nil {
			return nil, err
		}

		// Convert user details to ExtendedFriend type
		for _, user := range userDetails.Data {
			allFriends = append(allFriends, types.ExtendedFriend{
				Friend: apiTypes.Friend{
					ID: user.ID,
				},
				Name:             user.Name,
				DisplayName:      user.DisplayName,
				HasVerifiedBadge: user.HasVerifiedBadge,
			})
		}
	}

	f.logger.Debug("Finished fetching friend details",
		zap.Int("successfulFetches", len(allFriends)))

	return allFriends, nil
}

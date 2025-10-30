package fetcher

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/jaxron/roapi.go/pkg/api/resources/friends"
	"github.com/jaxron/roapi.go/pkg/api/resources/users"
	apiTypes "github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/robalyx/rotector/internal/database"
	"github.com/robalyx/rotector/pkg/utils"
	"github.com/sourcegraph/conc/pool"
	"go.uber.org/zap"
)

// FriendFetcher handles retrieval of user friend information from the Roblox API.
type FriendFetcher struct {
	db     database.Client
	roAPI  *api.API
	logger *zap.Logger
}

// NewFriendFetcher creates a FriendFetcher with the provided API client and logger.
func NewFriendFetcher(db database.Client, roAPI *api.API, logger *zap.Logger) *FriendFetcher {
	return &FriendFetcher{
		db:     db,
		roAPI:  roAPI,
		logger: logger.Named("friend_fetcher"),
	}
}

// GetFriendIDs returns the friend IDs for a user.
func (f *FriendFetcher) GetFriendIDs(ctx context.Context, userID int64) ([]int64, error) {
	// Get the friend count to determine which endpoint to use
	friendCount, err := f.roAPI.Friends().GetFriendCount(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get friend count: %w", err)
	}

	// For users with ≤200 friends, use the legacy endpoint and extract IDs
	if friendCount <= 200 {
		friendsData, err := f.getFriendsLegacy(ctx, userID)
		if err != nil {
			return nil, err
		}

		friendIDs := make([]int64, 0, len(friendsData))
		for _, friend := range friendsData {
			friendIDs = append(friendIDs, friend.ID)
		}

		return friendIDs, nil
	}

	// For users with >200 friends, use pagination to collect IDs
	var (
		friendIDs []int64
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
			if friend.ID != -1 {
				friendIDs = append(friendIDs, friend.ID)
			}
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
func (f *FriendFetcher) GetFriends(ctx context.Context, userID int64) ([]*apiTypes.ExtendedFriend, error) {
	// Get the friend count to determine which endpoint to use
	friendCount, err := f.roAPI.Friends().GetFriendCount(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get friend count: %w", err)
	}

	// For users with ≤200 friends, try the legacy endpoint
	if friendCount <= 200 {
		friends, err := f.getFriendsLegacy(ctx, userID)
		if err != nil {
			return nil, err
		}

		// Check if legacy endpoint returned invalid data
		hasEmptyNames := false

		for _, friend := range friends {
			if friend.ID != -1 && friend.Name == "" {
				hasEmptyNames = true
				break
			}
		}

		// Return data if valid
		if !hasEmptyNames {
			return friends, nil
		}

		f.logger.Warn("Legacy endpoint returned invalid data, falling back to pagination",
			zap.Int64("userID", userID))
	}

	// For users with >200 friends or invalid legacy data, get IDs then fetch details
	friendIDs, err := f.GetFriendIDs(ctx, userID)
	if err != nil {
		return nil, err
	}

	// Get existing friend info from database
	existingFriends, err := f.db.Model().User().GetRecentFriendInfos(ctx, friendIDs, time.Now().Add(-7*24*time.Hour))
	if err != nil {
		f.logger.Error("Failed to get existing friend info from database",
			zap.Error(err),
			zap.Int64("userID", userID))

		existingFriends = make(map[int64]*apiTypes.ExtendedFriend)
	}

	// Identify which friends need API lookup
	var missingIDs []int64

	for _, id := range friendIDs {
		if _, exists := existingFriends[id]; !exists {
			missingIDs = append(missingIDs, id)
		}
	}

	// If there are missing friends, fetch their details from API
	if len(missingIDs) > 0 {
		var (
			allFriends = make([]*apiTypes.ExtendedFriend, 0, len(missingIDs))
			p          = pool.New().WithContext(ctx)
			mu         sync.Mutex
			batchSize  = 100
		)

		// Process batches concurrently
		for i := 0; i < len(missingIDs); i += batchSize {
			end := min(i+batchSize, len(missingIDs))
			batchIDs := missingIDs[i:end]

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

				normalizer := utils.NewTextNormalizer()
				for _, user := range userDetails.Data {
					batchFriends = append(batchFriends, &apiTypes.ExtendedFriend{
						Friend: apiTypes.Friend{
							ID: user.ID,
						},
						Name:        normalizer.Normalize(user.Name),
						DisplayName: normalizer.Normalize(user.DisplayName),
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

		// Add newly fetched friends to existing map
		for _, friend := range allFriends {
			existingFriends[friend.ID] = friend
		}
	}

	// Convert map to slice
	result := make([]*apiTypes.ExtendedFriend, 0, len(friendIDs))
	for _, id := range friendIDs {
		if friend, ok := existingFriends[id]; ok {
			result = append(result, friend)
		}
	}

	f.logger.Debug("Finished fetching friends using pagination",
		zap.Int64("userID", userID),
		zap.Int("totalFriends", len(friendIDs)),
		zap.Int("successfulFetches", len(result)))

	return result, nil
}

// getFriendsLegacy returns a user's friends with full details using the legacy endpoint.
func (f *FriendFetcher) getFriendsLegacy(ctx context.Context, userID int64) ([]*apiTypes.ExtendedFriend, error) {
	response, err := f.roAPI.Friends().GetFriends(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get friends: %w", err)
	}

	normalizer := utils.NewTextNormalizer()
	friendsData := make([]*apiTypes.ExtendedFriend, 0, len(response.Data))

	for _, friend := range response.Data {
		if friend.ID != -1 {
			friendsData = append(friendsData, &apiTypes.ExtendedFriend{
				Friend: apiTypes.Friend{
					ID: friend.ID,
				},
				Name:        normalizer.Normalize(friend.Name),
				DisplayName: normalizer.Normalize(friend.DisplayName),
			})
		}
	}

	f.logger.Debug("Finished fetching friends using legacy endpoint",
		zap.Int64("userID", userID),
		zap.Int("totalFriends", len(friendsData)))

	return friendsData, nil
}

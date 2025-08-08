package fetcher

import (
	"context"
	"sync"
	"time"

	"github.com/jaxron/roapi.go/pkg/api"
	apiTypes "github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/setup"
	"github.com/robalyx/rotector/pkg/utils"
	"github.com/sourcegraph/conc/pool"
	"go.uber.org/zap"
)

// UserFetchResult contains all the data fetched for a user.
type UserFetchResult struct {
	Groups        []*apiTypes.UserGroupRoles
	Friends       []*apiTypes.ExtendedFriend
	Games         []*apiTypes.Game
	Outfits       []*apiTypes.Outfit
	OutfitAssets  map[int64][]*apiTypes.AssetV2
	CurrentAssets []*apiTypes.AssetV2
}

// UserFetcher handles concurrent retrieval of user information from the Roblox API.
type UserFetcher struct {
	roAPI            *api.API
	logger           *zap.Logger
	groupFetcher     *GroupFetcher
	gameFetcher      *GameFetcher
	friendFetcher    *FriendFetcher
	outfitFetcher    *OutfitFetcher
	thumbnailFetcher *ThumbnailFetcher
	inventoryFetcher *InventoryFetcher
}

// NewUserFetcher creates a UserFetcher with the provided API client and logger.
func NewUserFetcher(app *setup.App, logger *zap.Logger) *UserFetcher {
	return &UserFetcher{
		roAPI:            app.RoAPI,
		logger:           logger.Named("user_fetcher"),
		groupFetcher:     NewGroupFetcher(app.RoAPI, logger),
		gameFetcher:      NewGameFetcher(app.RoAPI, logger),
		friendFetcher:    NewFriendFetcher(app, logger),
		outfitFetcher:    NewOutfitFetcher(app.RoAPI, logger),
		thumbnailFetcher: NewThumbnailFetcher(app.RoAPI, logger),
		inventoryFetcher: NewInventoryFetcher(app.RoAPI, logger),
	}
}

// FetchInfos retrieves complete user information for a batch of user IDs.
func (u *UserFetcher) FetchInfos(ctx context.Context, userIDs []int64) []*types.ReviewUser {
	var (
		validUsers = make([]*types.ReviewUser, 0, len(userIDs))
		userMap    = make(map[int64]*types.User)
		p          = pool.New().WithContext(ctx)
		mu         sync.Mutex
	)

	// Process each user concurrently
	for _, id := range userIDs {
		p.Go(func(ctx context.Context) error {
			// Fetch the user info
			userInfo, err := u.roAPI.Users().GetUserByID(ctx, id)
			if err != nil {
				u.logger.Error("Error fetching user info",
					zap.Int64("userID", id),
					zap.Error(err))

				return nil // Don't fail the whole batch for one error
			}

			// Skip banned users
			if userInfo.IsBanned {
				return nil
			}

			// Fetch user data concurrently
			fetchResult := u.fetchUserData(ctx, id)

			// Add user to map for thumbnail fetching
			mu.Lock()

			userMap[id] = &types.User{ID: id}

			mu.Unlock()

			// Add the user info to valid users
			normalizer := utils.NewTextNormalizer()
			now := time.Now()
			user := &types.ReviewUser{
				User: &types.User{
					ID:           userInfo.ID,
					Name:         normalizer.Normalize(userInfo.Name),
					DisplayName:  normalizer.Normalize(userInfo.DisplayName),
					Description:  normalizer.Normalize(userInfo.Description),
					CreatedAt:    userInfo.Created,
					LastUpdated:  now,
					LastBanCheck: now,
				},
				Groups:        fetchResult.Groups,
				Friends:       fetchResult.Friends,
				Games:         fetchResult.Games,
				Outfits:       fetchResult.Outfits,
				OutfitAssets:  fetchResult.OutfitAssets,
				CurrentAssets: fetchResult.CurrentAssets,
				Inventory:     []*apiTypes.InventoryAsset{},
				Favorites:     []*apiTypes.Game{},
				Badges:        []any{},
			}

			mu.Lock()

			validUsers = append(validUsers, user)

			mu.Unlock()

			return nil
		})
	}

	// Wait for all goroutines to complete
	if err := p.Wait(); err != nil {
		u.logger.Error("Error during user fetch", zap.Error(err))
	}

	// Check if user map is empty
	if len(userMap) == 0 {
		return validUsers
	}

	// Fetch thumbnails for all valid users
	thumbnails := u.thumbnailFetcher.GetImageURLs(ctx, userMap)

	// Add thumbnails to the corresponding user info
	for _, user := range validUsers {
		if thumbnailURL, ok := thumbnails[user.ID]; ok {
			user.ThumbnailURL = thumbnailURL
			user.LastThumbnailUpdate = time.Now()
		}
	}

	u.logger.Debug("Finished fetching user information",
		zap.Int("totalRequested", len(userIDs)),
		zap.Int("successfulFetches", len(validUsers)))

	return validUsers
}

// FetchBannedUsers checks which users from a batch of IDs are currently banned.
// Returns a slice of banned user IDs.
func (u *UserFetcher) FetchBannedUsers(ctx context.Context, userIDs []int64) ([]int64, error) {
	var (
		results = make([]int64, 0, len(userIDs))
		p       = pool.New().WithContext(ctx)
		mu      sync.Mutex
	)

	// Process each user concurrently
	for _, id := range userIDs {
		p.Go(func(ctx context.Context) error {
			userInfo, err := u.roAPI.Users().GetUserByID(ctx, id)
			if err != nil {
				u.logger.Error("Error fetching user info",
					zap.Int64("userID", id),
					zap.Error(err))

				return nil // Don't fail the whole batch for one error
			}

			if userInfo.IsBanned {
				mu.Lock()

				results = append(results, userInfo.ID)

				mu.Unlock()
			}

			return nil
		})
	}

	// Wait for all goroutines to complete
	if err := p.Wait(); err != nil {
		u.logger.Error("Error during banned users fetch", zap.Error(err))
		return nil, err
	}

	u.logger.Debug("Finished checking banned users",
		zap.Int("totalChecked", len(userIDs)),
		zap.Int("bannedUsers", len(results)))

	return results, nil
}

// fetchUserData retrieves a user's group memberships, friend list, and games concurrently.
func (u *UserFetcher) fetchUserData(ctx context.Context, userID int64) *UserFetchResult {
	result := &UserFetchResult{
		OutfitAssets: make(map[int64][]*apiTypes.AssetV2),
	}
	p := pool.New().WithContext(ctx)

	// Fetch user's groups
	p.Go(func(ctx context.Context) error {
		var err error

		result.Groups, err = u.groupFetcher.GetUserGroups(ctx, userID)
		if err != nil {
			u.logger.Warn("Failed to fetch user groups",
				zap.Error(err),
				zap.Int64("userID", userID))
		}

		return nil
	})

	// Fetch user's friends
	p.Go(func(ctx context.Context) error {
		var err error

		result.Friends, err = u.friendFetcher.GetFriends(ctx, userID)
		if err != nil {
			u.logger.Warn("Failed to fetch user friends",
				zap.Error(err),
				zap.Int64("userID", userID))
		}

		return nil
	})

	// Fetch user's games
	p.Go(func(ctx context.Context) error {
		var err error

		result.Games, err = u.gameFetcher.FetchGamesForUser(ctx, userID)
		if err != nil {
			u.logger.Warn("Failed to fetch user games",
				zap.Error(err),
				zap.Int64("userID", userID))
		}

		return nil
	})

	// Fetch user's outfits
	p.Go(func(ctx context.Context) error {
		outfits, currentAssets, err := u.outfitFetcher.GetOutfits(ctx, userID)
		if err != nil {
			u.logger.Warn("Failed to fetch user outfits",
				zap.Error(err),
				zap.Int64("userID", userID))

			return nil
		}

		result.Outfits = outfits
		result.CurrentAssets = currentAssets

		return nil
	})

	// Fetch user's inventory
	// p.Go(func(ctx context.Context) error {
	// 	var err error
	// 	inventory, err = u.inventoryFetcher.GetInventory(ctx, userID)
	// 	if err != nil {
	// 		if strings.Contains(err.Error(), "You are not authorized to view this user's inventory.") {
	// 			return nil
	// 		}
	// 		u.logger.Warn("Failed to fetch user inventory",
	// 			zap.Error(err),
	// 			zap.Int64("userID", userID))
	// 	}
	// 	return nil
	// })

	// Wait for all fetches to complete
	_ = p.Wait()

	return result
}

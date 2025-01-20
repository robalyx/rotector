package fetcher

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/jaxron/roapi.go/pkg/api"
	apiTypes "github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/robalyx/rotector/internal/common/setup"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"go.uber.org/zap"
)

// ErrUserBanned indicates that the user is banned from Roblox.
var ErrUserBanned = errors.New("user is banned")

// UserFetchResult contains the result of fetching a user's information.
type UserFetchResult struct {
	ID    uint64
	Info  *Info
	Error error
}

// UserGroupFetchResult contains the result of fetching a user's groups.
type UserGroupFetchResult struct {
	Data  []*apiTypes.UserGroupRoles
	Error error
}

// UserFriendFetchResult contains the result of fetching a user's friends.
type UserFriendFetchResult struct {
	Data  []*types.ExtendedFriend
	Error error
}

// UserGamesFetchResult contains the result of fetching a user's games.
type UserGamesFetchResult struct {
	Data  []*apiTypes.Game
	Error error
}

// Info combines user profile data with their group memberships and friend list.
type Info struct {
	ID             uint64                 `json:"id"`
	Name           string                 `json:"name"`
	DisplayName    string                 `json:"displayName"`
	Description    string                 `json:"description"`
	CreatedAt      time.Time              `json:"createdAt"`
	Groups         *UserGroupFetchResult  `json:"groupIds"`
	Friends        *UserFriendFetchResult `json:"friends"`
	Games          *UserGamesFetchResult  `json:"games"`
	FollowerCount  uint64                 `json:"followerCount"`
	FollowingCount uint64                 `json:"followingCount"`
	LastUpdated    time.Time              `json:"lastUpdated"`
	LastBanCheck   time.Time              `json:"lastBanCheck"`
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
	followFetcher    *FollowFetcher
}

// NewUserFetcher creates a UserFetcher with the provided API client and logger.
func NewUserFetcher(app *setup.App, logger *zap.Logger) *UserFetcher {
	return &UserFetcher{
		roAPI:            app.RoAPI,
		logger:           logger,
		groupFetcher:     NewGroupFetcher(app.RoAPI, logger),
		gameFetcher:      NewGameFetcher(app.RoAPI, logger),
		friendFetcher:    NewFriendFetcher(app.RoAPI, logger),
		outfitFetcher:    NewOutfitFetcher(app.RoAPI, logger),
		thumbnailFetcher: NewThumbnailFetcher(app.RoAPI, logger),
		followFetcher:    NewFollowFetcher(app.RoAPI, logger),
	}
}

// FetchInfos retrieves complete user information for a batch of user IDs.
func (u *UserFetcher) FetchInfos(userIDs []uint64) []*Info {
	var (
		validUsers = make([]*Info, 0, len(userIDs))
		mu         sync.Mutex
		wg         sync.WaitGroup
	)

	// Process each user concurrently
	for _, userID := range userIDs {
		wg.Add(1)
		go func(id uint64) {
			defer wg.Done()

			// Fetch the user info
			userInfo, err := u.roAPI.Users().GetUserByID(context.Background(), id)
			if err != nil {
				u.logger.Error("Error fetching user info",
					zap.Uint64("userID", id),
					zap.Error(err))
				return
			}

			// Skip banned users
			if userInfo.IsBanned {
				return
			}

			// Fetch groups, friends, and games concurrently
			groups, friends, games := u.fetchUserData(id)

			// Add the user info to valid users
			now := time.Now()
			info := &Info{
				ID:           userInfo.ID,
				Name:         userInfo.Name,
				DisplayName:  userInfo.DisplayName,
				Description:  userInfo.Description,
				CreatedAt:    userInfo.Created,
				Groups:       groups,
				Friends:      friends,
				Games:        games,
				LastUpdated:  now,
				LastBanCheck: now,
			}

			mu.Lock()
			validUsers = append(validUsers, info)
			mu.Unlock()
		}(userID)
	}

	wg.Wait()

	u.logger.Debug("Finished fetching user information",
		zap.Int("totalRequested", len(userIDs)),
		zap.Int("successfulFetches", len(validUsers)))

	return validUsers
}

// fetchUserData retrieves a user's group memberships, friend list, and games concurrently.
func (u *UserFetcher) fetchUserData(userID uint64) (*UserGroupFetchResult, *UserFriendFetchResult, *UserGamesFetchResult) {
	var (
		groupResult  *UserGroupFetchResult
		friendResult *UserFriendFetchResult
		gameResult   *UserGamesFetchResult
		wg           sync.WaitGroup
	)

	wg.Add(3)

	// Fetch user's groups
	go func() {
		defer wg.Done()
		groups, err := u.groupFetcher.GetUserGroups(context.Background(), userID)
		groupResult = &UserGroupFetchResult{
			Data:  groups,
			Error: err,
		}
	}()

	// Fetch user's friends
	go func() {
		defer wg.Done()
		fetchedFriends, err := u.friendFetcher.GetFriendsWithDetails(context.Background(), userID)
		friendResult = &UserFriendFetchResult{
			Data:  fetchedFriends,
			Error: err,
		}
	}()

	// Fetch user's games
	go func() {
		defer wg.Done()
		games, err := u.gameFetcher.FetchGamesForUser(userID)
		gameResult = &UserGamesFetchResult{
			Data:  games,
			Error: err,
		}
	}()

	wg.Wait()
	return groupResult, friendResult, gameResult
}

// FetchBannedUsers checks which users from a batch of IDs are currently banned.
// Returns a slice of banned user IDs.
func (u *UserFetcher) FetchBannedUsers(userIDs []uint64) ([]uint64, error) {
	var (
		results = make([]uint64, 0, len(userIDs))
		mu      sync.Mutex
		wg      sync.WaitGroup
	)

	for _, userID := range userIDs {
		wg.Add(1)
		go func(id uint64) {
			defer wg.Done()

			userInfo, err := u.roAPI.Users().GetUserByID(context.Background(), id)
			if err != nil {
				u.logger.Warn("Error fetching user info",
					zap.Uint64("userID", id),
					zap.Error(err))
				return
			}

			if userInfo.IsBanned {
				mu.Lock()
				results = append(results, userInfo.ID)
				mu.Unlock()
			}
		}(userID)
	}

	wg.Wait()

	u.logger.Debug("Finished checking banned users",
		zap.Int("totalChecked", len(userIDs)),
		zap.Int("bannedUsers", len(results)))

	return results, nil
}

// FetchAdditionalUserData concurrently fetches thumbnails, outfits, and follow counts for users.
func (u *UserFetcher) FetchAdditionalUserData(users map[uint64]*types.User) map[uint64]*types.User {
	var (
		now = time.Now()
		mu  sync.Mutex
		wg  sync.WaitGroup
	)

	wg.Add(3)

	// Fetch data concurrently
	go func() {
		defer wg.Done()
		images := u.thumbnailFetcher.AddImageURLs(users)
		mu.Lock()
		for id, url := range images {
			if user, ok := users[id]; ok {
				user.ThumbnailURL = url
				user.LastThumbnailUpdate = now
			}
		}
		mu.Unlock()
	}()

	go func() {
		defer wg.Done()
		outfits := u.outfitFetcher.AddOutfits(users)
		mu.Lock()
		for id, result := range outfits {
			if result.Error == nil {
				if user, ok := users[id]; ok {
					user.Outfits = result.Outfits.Data
				}
			}
		}
		mu.Unlock()
	}()

	go func() {
		defer wg.Done()
		follows := u.followFetcher.AddFollowCounts(users)
		mu.Lock()
		for id, result := range follows {
			if result.Error == nil {
				if user, ok := users[id]; ok {
					user.FollowerCount = result.FollowerCount
					user.FollowingCount = result.FollowingCount
				}
			}
		}
		mu.Unlock()
	}()

	wg.Wait()

	return users
}

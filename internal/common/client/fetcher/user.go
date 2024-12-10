package fetcher

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/jaxron/roapi.go/pkg/api/resources/groups"
	apiTypes "github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/rotector/rotector/internal/common/setup"
	"github.com/rotector/rotector/internal/common/storage/database/types"
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
	Data  []types.ExtendedFriend
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
}

// UserFetcher handles concurrent retrieval of user information from the Roblox API.
type UserFetcher struct {
	roAPI            *api.API
	logger           *zap.Logger
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
		gameFetcher:      NewGameFetcher(app.RoAPI, logger),
		friendFetcher:    NewFriendFetcher(app.RoAPI, logger),
		outfitFetcher:    NewOutfitFetcher(app.RoAPI, logger),
		thumbnailFetcher: NewThumbnailFetcher(app.RoAPI, logger),
		followFetcher:    NewFollowFetcher(app.RoAPI, logger),
	}
}

// FetchInfos retrieves complete user information for a batch of user IDs.
// It skips banned users and fetches groups/friends/games concurrently for each user.
func (u *UserFetcher) FetchInfos(userIDs []uint64) []*Info {
	var wg sync.WaitGroup
	resultsChan := make(chan UserFetchResult, len(userIDs))

	// Process each user concurrently
	for _, userID := range userIDs {
		wg.Add(1)
		go func(id uint64) {
			defer wg.Done()

			// Fetch the user info
			userInfo, err := u.roAPI.Users().GetUserByID(context.Background(), id)
			if err != nil {
				resultsChan <- UserFetchResult{
					ID:    id,
					Error: err,
				}
				return
			}

			// Skip banned users
			if userInfo.IsBanned {
				resultsChan <- UserFetchResult{
					ID:    id,
					Error: ErrUserBanned,
				}
				return
			}

			// Fetch groups, friends, and games concurrently
			groups, friends, games := u.fetchUserData(id)

			// Send the user info to the channel
			resultsChan <- UserFetchResult{
				ID: id,
				Info: &Info{
					ID:          userInfo.ID,
					Name:        userInfo.Name,
					DisplayName: userInfo.DisplayName,
					Description: userInfo.Description,
					CreatedAt:   userInfo.Created,
					Groups:      groups,
					Friends:     friends,
					Games:       games,
					LastUpdated: time.Now(),
				},
			}
		}(userID)
	}

	// Close channel when all goroutines complete
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// Collect results from the channel
	results := make(map[uint64]*UserFetchResult)
	for result := range resultsChan {
		results[result.ID] = &result
	}

	// Process results and filter out errors
	validUsers := make([]*Info, 0, len(userIDs))
	for userID, result := range results {
		if result.Error != nil {
			if !errors.Is(result.Error, ErrUserBanned) {
				u.logger.Warn("Error fetching user info",
					zap.Uint64("userID", userID),
					zap.Error(result.Error))
			}
			continue
		}

		if result.Info != nil {
			validUsers = append(validUsers, result.Info)
		}
	}

	u.logger.Debug("Finished fetching user information",
		zap.Int("totalRequested", len(userIDs)),
		zap.Int("successfulFetches", len(validUsers)))

	return validUsers
}

// fetchUserData retrieves a user's group memberships, friend list, games, follower count, and following count concurrently.
func (u *UserFetcher) fetchUserData(userID uint64) (*UserGroupFetchResult, *UserFriendFetchResult, *UserGamesFetchResult) {
	var wg sync.WaitGroup
	wg.Add(3)

	groupChan := make(chan *UserGroupFetchResult, 1)
	friendChan := make(chan *UserFriendFetchResult, 1)
	gameChan := make(chan *UserGamesFetchResult, 1)

	// Fetch user's groups
	go func() {
		defer wg.Done()
		builder := groups.NewUserGroupRolesBuilder(userID)
		fetchedGroups, err := u.roAPI.Groups().GetUserGroupRoles(context.Background(), builder.Build())
		if err != nil {
			groupChan <- &UserGroupFetchResult{Error: err}
			return
		}

		groups := make([]*apiTypes.UserGroupRoles, 0, len(fetchedGroups.Data))
		for _, group := range fetchedGroups.Data {
			groups = append(groups, &group)
		}
		groupChan <- &UserGroupFetchResult{Data: groups}
	}()

	// Fetch user's friends
	go func() {
		defer wg.Done()
		fetchedFriends, err := u.friendFetcher.GetFriendsWithDetails(context.Background(), userID)
		friendChan <- &UserFriendFetchResult{
			Data:  fetchedFriends,
			Error: err,
		}
	}()

	// Fetch user's games
	go func() {
		defer wg.Done()
		games, err := u.gameFetcher.FetchGamesForUser(userID)
		gameChan <- &UserGamesFetchResult{
			Data:  games,
			Error: err,
		}
	}()

	// Process results as they arrive
	var groupResult *UserGroupFetchResult
	var friendResult *UserFriendFetchResult
	var gameResult *UserGamesFetchResult

	remaining := 3
	for remaining > 0 {
		select {
		case result := <-groupChan:
			if result.Error != nil {
				u.logger.Warn("Error fetching user groups",
					zap.Uint64("userID", userID),
					zap.Error(result.Error))
			}
			groupResult = result
			remaining--

		case result := <-friendChan:
			if result.Error != nil {
				u.logger.Warn("Error fetching user friends",
					zap.Uint64("userID", userID),
					zap.Error(result.Error))
			}
			friendResult = result
			remaining--

		case result := <-gameChan:
			if result.Error != nil {
				u.logger.Warn("Error fetching user games",
					zap.Uint64("userID", userID),
					zap.Error(result.Error))
			}
			gameResult = result
			remaining--
		}
	}

	// Wait for all goroutines to finish and close channels
	wg.Wait()
	close(groupChan)
	close(friendChan)
	close(gameChan)

	return groupResult, friendResult, gameResult
}

// FetchBannedUsers checks which users from a batch of IDs are currently banned.
// Returns a slice of banned user IDs.
func (u *UserFetcher) FetchBannedUsers(userIDs []uint64) ([]uint64, error) {
	var wg sync.WaitGroup
	results := make([]uint64, 0, len(userIDs))
	userChan := make(chan uint64, len(userIDs))

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
				userChan <- userInfo.ID
			}
		}(userID)
	}

	go func() {
		wg.Wait()
		close(userChan)
	}()

	for id := range userChan {
		results = append(results, id)
	}

	u.logger.Debug("Finished checking banned users",
		zap.Int("totalChecked", len(userIDs)),
		zap.Int("bannedUsers", len(results)))

	return results, nil
}

// FetchAdditionalUserData concurrently fetches thumbnails, outfits, and follow counts for users.
func (u *UserFetcher) FetchAdditionalUserData(users map[uint64]*types.User) map[uint64]*types.User {
	var wg sync.WaitGroup
	wg.Add(3)

	// Create channels for results
	imagesChan := make(chan map[uint64]string, 1)
	outfitsChan := make(chan map[uint64]*OutfitFetchResult, 1)
	followsChan := make(chan map[uint64]*FollowFetchResult, 1)

	// Fetch data concurrently
	go func() {
		defer wg.Done()
		imagesChan <- u.thumbnailFetcher.AddImageURLs(users)
	}()

	go func() {
		defer wg.Done()
		outfitsChan <- u.outfitFetcher.AddOutfits(users)
	}()

	go func() {
		defer wg.Done()
		followsChan <- u.followFetcher.AddFollowCounts(users)
	}()

	// Process results as they arrive
	remaining := 3
	for remaining > 0 {
		select {
		case images := <-imagesChan:
			for id, url := range images {
				if user, ok := users[id]; ok {
					user.ThumbnailURL = url
				}
			}
			remaining--

		case outfits := <-outfitsChan:
			for id, result := range outfits {
				if result.Error == nil {
					if user, ok := users[id]; ok {
						user.Outfits = result.Outfits.Data
					}
				}
			}
			remaining--

		case follows := <-followsChan:
			for id, result := range follows {
				if result.Error == nil {
					if user, ok := users[id]; ok {
						user.FollowerCount = result.FollowerCount
						user.FollowingCount = result.FollowingCount
					}
				}
			}
			remaining--
		}
	}

	// Wait for all goroutines to finish and close channels
	wg.Wait()
	close(imagesChan)
	close(outfitsChan)
	close(followsChan)

	return users
}

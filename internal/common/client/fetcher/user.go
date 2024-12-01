package fetcher

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/jaxron/roapi.go/pkg/api/resources/groups"
	"github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/rotector/rotector/internal/common/setup"
	"github.com/rotector/rotector/internal/common/storage/database/models"
	"go.uber.org/zap"
)

// ErrUserBanned indicates that the user is banned from Roblox.
var ErrUserBanned = errors.New("user is banned")

// UserGroupFetchResult contains the result of fetching a user's groups.
type UserGroupFetchResult struct {
	Data  []*types.UserGroupRoles
	Error error
}

// UserFriendFetchResult contains the result of fetching a user's friends.
type UserFriendFetchResult struct {
	Data  []models.ExtendedFriend
	Error error
}

// UserGamesFetchResult contains the result of fetching a user's games.
type UserGamesFetchResult struct {
	Data  []*types.Game
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
	roAPI         *api.API
	logger        *zap.Logger
	gameFetcher   *GameFetcher
	friendFetcher *FriendFetcher
}

// NewUserFetcher creates a UserFetcher with the provided API client and logger.
func NewUserFetcher(app *setup.App, logger *zap.Logger) *UserFetcher {
	return &UserFetcher{
		roAPI:         app.RoAPI,
		logger:        logger,
		gameFetcher:   NewGameFetcher(app.RoAPI, logger),
		friendFetcher: NewFriendFetcher(app.RoAPI, logger),
	}
}

// FetchInfos retrieves complete user information for a batch of user IDs.
// It skips banned users and fetches groups/friends/games concurrently for each user.
func (u *UserFetcher) FetchInfos(userIDs []uint64) []*Info {
	// UserFetchResult contains the result of fetching a user's information.
	type UserFetchResult struct {
		Info  *Info
		Error error
	}

	var wg sync.WaitGroup
	resultsChan := make(chan struct {
		UserID uint64
		Result *UserFetchResult
	}, len(userIDs))

	// Process each user concurrently
	for _, userID := range userIDs {
		wg.Add(1)
		go func(id uint64) {
			defer wg.Done()

			// Fetch the user info
			userInfo, err := u.roAPI.Users().GetUserByID(context.Background(), id)
			if err != nil {
				resultsChan <- struct {
					UserID uint64
					Result *UserFetchResult
				}{
					UserID: id,
					Result: &UserFetchResult{Error: err},
				}
				return
			}

			// Skip banned users
			if userInfo.IsBanned {
				resultsChan <- struct {
					UserID uint64
					Result *UserFetchResult
				}{
					UserID: id,
					Result: &UserFetchResult{Error: ErrUserBanned},
				}
				return
			}

			// Fetch groups, friends, and games concurrently
			groups, friends, games, followerCount, followingCount := u.fetchUserData(id)

			// Send the user info to the channel
			resultsChan <- struct {
				UserID uint64
				Result *UserFetchResult
			}{
				UserID: id,
				Result: &UserFetchResult{
					Info: &Info{
						ID:             userInfo.ID,
						Name:           userInfo.Name,
						DisplayName:    userInfo.DisplayName,
						Description:    userInfo.Description,
						CreatedAt:      userInfo.Created,
						Groups:         groups,
						Friends:        friends,
						Games:          games,
						FollowerCount:  followerCount,
						FollowingCount: followingCount,
						LastUpdated:    time.Now(),
					},
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
		results[result.UserID] = result.Result
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
func (u *UserFetcher) fetchUserData(userID uint64) (*UserGroupFetchResult, *UserFriendFetchResult, *UserGamesFetchResult, uint64, uint64) {
	var wg sync.WaitGroup
	groupChan := make(chan *UserGroupFetchResult, 1)
	friendChan := make(chan *UserFriendFetchResult, 1)
	gameChan := make(chan *UserGamesFetchResult, 1)
	followerChan := make(chan uint64, 1)
	followingChan := make(chan uint64, 1)

	// Fetch user's groups
	wg.Add(1)
	go func() {
		defer wg.Done()
		u.fetchGroups(userID, groupChan)
	}()

	// Fetch user's friends
	wg.Add(1)
	go func() {
		defer wg.Done()
		u.fetchFriends(userID, friendChan)
	}()

	// Fetch user's games
	wg.Add(1)
	go func() {
		defer wg.Done()
		u.fetchGames(userID, gameChan)
	}()

	// Fetch follower count
	wg.Add(1)
	go func() {
		defer wg.Done()
		u.fetchFollowerCount(userID, followerChan)
	}()

	// Fetch following count
	wg.Add(1)
	go func() {
		defer wg.Done()
		u.fetchFollowingCount(userID, followingChan)
	}()

	// Wait for all goroutines to complete
	wg.Wait()

	// Get results
	groupResult := <-groupChan
	friendResult := <-friendChan
	gameResult := <-gameChan
	followerCount := <-followerChan
	followingCount := <-followingChan

	if groupResult.Error != nil {
		u.logger.Warn("Error fetching user groups",
			zap.Uint64("userID", userID),
			zap.Error(groupResult.Error))
	}

	if friendResult.Error != nil {
		u.logger.Warn("Error fetching user friends",
			zap.Uint64("userID", userID),
			zap.Error(friendResult.Error))
	}

	if gameResult.Error != nil {
		u.logger.Warn("Error fetching user games",
			zap.Uint64("userID", userID),
			zap.Error(gameResult.Error))
	}

	return groupResult, friendResult, gameResult, followerCount, followingCount
}

// fetchGroups retrieves a user's group memberships.
func (u *UserFetcher) fetchGroups(userID uint64, groupChan chan *UserGroupFetchResult) {
	defer close(groupChan)
	builder := groups.NewUserGroupRolesBuilder(userID)
	fetchedGroups, err := u.roAPI.Groups().GetUserGroupRoles(context.Background(), builder.Build())
	if err != nil {
		groupChan <- &UserGroupFetchResult{Error: err}
		return
	}

	groups := make([]*types.UserGroupRoles, 0, len(fetchedGroups.Data))
	for _, group := range fetchedGroups.Data {
		groups = append(groups, &group)
	}
	groupChan <- &UserGroupFetchResult{Data: groups}
}

// fetchFriends retrieves a user's friend list.
func (u *UserFetcher) fetchFriends(userID uint64, friendChan chan *UserFriendFetchResult) {
	defer close(friendChan)
	fetchedFriends, err := u.friendFetcher.GetFriends(context.Background(), userID)
	friendChan <- &UserFriendFetchResult{
		Data:  fetchedFriends,
		Error: err,
	}
}

// fetchGames retrieves a user's games.
func (u *UserFetcher) fetchGames(userID uint64, gameChan chan *UserGamesFetchResult) {
	defer close(gameChan)
	games, err := u.gameFetcher.FetchGamesForUser(userID)
	gameChan <- &UserGamesFetchResult{
		Data:  games,
		Error: err,
	}
}

// fetchFollowerCount retrieves a user's follower count.
func (u *UserFetcher) fetchFollowerCount(userID uint64, followerChan chan uint64) {
	defer close(followerChan)
	count, err := u.roAPI.Friends().GetFollowerCount(context.Background(), userID)
	if err != nil {
		u.logger.Warn("Error fetching follower count",
			zap.Uint64("userID", userID),
			zap.Error(err))
		followerChan <- 0
		return
	}
	followerChan <- count
}

// fetchFollowingCount retrieves a user's following count.
func (u *UserFetcher) fetchFollowingCount(userID uint64, followingChan chan uint64) {
	defer close(followingChan)
	count, err := u.roAPI.Friends().GetFollowingCount(context.Background(), userID)
	if err != nil {
		u.logger.Warn("Error fetching following count",
			zap.Uint64("userID", userID),
			zap.Error(err))
		followingChan <- 0
		return
	}
	followingChan <- count
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

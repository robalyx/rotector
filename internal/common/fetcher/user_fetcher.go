package fetcher

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/jaxron/roapi.go/pkg/api/resources/groups"
	"github.com/jaxron/roapi.go/pkg/api/types"
	"go.uber.org/zap"
)

// ErrUserBanned indicates that the user is banned from Roblox.
var ErrUserBanned = errors.New("user is banned")

// Info combines user profile data with their group memberships and friend list.
type Info struct {
	ID          uint64                 `json:"id"`
	Name        string                 `json:"name"`
	DisplayName string                 `json:"displayName"`
	Description string                 `json:"description"`
	CreatedAt   time.Time              `json:"createdAt"`
	Groups      []types.UserGroupRoles `json:"groupIds"`
	Friends     []types.Friend         `json:"friends"`
	LastUpdated time.Time              `json:"lastUpdated"`
}

// UserFetchResult contains the result of fetching a user's information.
type UserFetchResult struct {
	Info  *Info
	Error error
}

// FriendFetchResult contains the result of fetching a user's friends.
type FriendFetchResult struct {
	Friends []types.Friend
	Error   error
}

// UserGroupFetchResult contains the result of fetching a user's groups.
type UserGroupFetchResult struct {
	Groups []types.UserGroupRoles
	Error  error
}

// UserFetcher handles concurrent retrieval of user information from the Roblox API.
type UserFetcher struct {
	roAPI  *api.API
	logger *zap.Logger
}

// NewUserFetcher creates a UserFetcher with the provided API client and logger.
func NewUserFetcher(roAPI *api.API, logger *zap.Logger) *UserFetcher {
	return &UserFetcher{
		roAPI:  roAPI,
		logger: logger,
	}
}

// FetchInfos retrieves complete user information for a batch of user IDs.
// It skips banned users and fetches groups/friends concurrently for each user.
func (u *UserFetcher) FetchInfos(userIDs []uint64) []*Info {
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

			// Fetch groups and friends concurrently
			groups, friends := u.fetchGroupsAndFriends(id)

			// Send the user info to the channel
			resultsChan <- struct {
				UserID uint64
				Result *UserFetchResult
			}{
				UserID: id,
				Result: &UserFetchResult{
					Info: &Info{
						ID:          userInfo.ID,
						Name:        userInfo.Name,
						DisplayName: userInfo.DisplayName,
						Description: userInfo.Description,
						CreatedAt:   userInfo.Created,
						Groups:      groups,
						Friends:     friends,
						LastUpdated: time.Now(),
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

	u.logger.Info("Finished fetching user information",
		zap.Int("totalRequested", len(userIDs)),
		zap.Int("successfulFetches", len(validUsers)))

	return validUsers
}

// fetchGroupsAndFriends retrieves a user's group memberships and friend list concurrently.
func (u *UserFetcher) fetchGroupsAndFriends(userID uint64) ([]types.UserGroupRoles, []types.Friend) {
	var wg sync.WaitGroup
	groupChan := make(chan *UserGroupFetchResult, 1)
	friendChan := make(chan *FriendFetchResult, 1)

	// Fetch user's groups
	wg.Add(1)
	go func() {
		defer wg.Done()
		builder := groups.NewUserGroupRolesBuilder(userID)
		fetchedGroups, err := u.roAPI.Groups().GetUserGroupRoles(context.Background(), builder.Build())
		groupChan <- &UserGroupFetchResult{
			Groups: fetchedGroups,
			Error:  err,
		}
	}()

	// Fetch user's friends
	wg.Add(1)
	go func() {
		defer wg.Done()
		fetchedFriends, err := u.roAPI.Friends().GetFriends(context.Background(), userID)
		friendChan <- &FriendFetchResult{
			Friends: fetchedFriends,
			Error:   err,
		}
	}()

	// Wait for both goroutines and close channels
	go func() {
		wg.Wait()
		close(groupChan)
		close(friendChan)
	}()

	// Get results
	groupResult := <-groupChan
	friendResult := <-friendChan

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

	return groupResult.Groups, friendResult.Friends
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

	u.logger.Info("Finished checking banned users",
		zap.Int("totalChecked", len(userIDs)),
		zap.Int("bannedUsers", len(results)))

	return results, nil
}

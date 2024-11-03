package fetcher

import (
	"context"
	"sync"
	"time"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/jaxron/roapi.go/pkg/api/resources/groups"
	"github.com/jaxron/roapi.go/pkg/api/types"
	"go.uber.org/zap"
)

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
	resultChan := make(chan *Info, len(userIDs))
	results := make([]*Info, 0, len(userIDs))

	// Process each user concurrently
	for _, userID := range userIDs {
		wg.Add(1)
		go func(id uint64) {
			defer wg.Done()

			// Fetch the user info
			userInfo, err := u.roAPI.Users().GetUserByID(context.Background(), id)
			if err != nil {
				u.logger.Warn("Error fetching user info",
					zap.Uint64("userID", id),
					zap.Error(err))
				return
			}

			// Skip banned users
			if userInfo.IsBanned {
				return
			}

			// Fetch groups and friends concurrently
			groups, friends := u.fetchGroupsAndFriends(id)

			// Send the user info to the channel
			resultChan <- &Info{
				ID:          userInfo.ID,
				Name:        userInfo.Name,
				DisplayName: userInfo.DisplayName,
				Description: userInfo.Description,
				CreatedAt:   userInfo.Created,
				Groups:      groups,
				Friends:     friends,
				LastUpdated: time.Now(),
			}
		}(userID)
	}

	// Use a separate goroutine to close the channel
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Collect results as they arrive
	for info := range resultChan {
		results = append(results, info)
	}

	u.logger.Info("Finished fetching user information",
		zap.Int("totalRequested", len(userIDs)),
		zap.Int("successfulFetches", len(results)))

	return results
}

// fetchGroupsAndFriends retrieves a user's group memberships and friend list
// concurrently. Returns empty slices if all requests fail.
func (u *UserFetcher) fetchGroupsAndFriends(userID uint64) ([]types.UserGroupRoles, []types.Friend) {
	type result struct {
		groups  []types.UserGroupRoles
		friends []types.Friend
		err     error
	}

	var wg sync.WaitGroup
	groupChan := make(chan result, 1)
	friendChan := make(chan result, 1)

	// Fetch user's groups
	wg.Add(1)
	go func() {
		defer wg.Done()
		builder := groups.NewUserGroupRolesBuilder(userID)
		fetchedGroups, err := u.roAPI.Groups().GetUserGroupRoles(context.Background(), builder.Build())
		groupChan <- result{groups: fetchedGroups, err: err}
	}()

	// Fetch user's friends
	wg.Add(1)
	go func() {
		defer wg.Done()
		fetchedFriends, err := u.roAPI.Friends().GetFriends(context.Background(), userID)
		friendChan <- result{friends: fetchedFriends, err: err}
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

	if groupResult.err != nil {
		u.logger.Warn("Error fetching user groups",
			zap.Uint64("userID", userID),
			zap.Error(groupResult.err))
	}

	if friendResult.err != nil {
		u.logger.Warn("Error fetching user friends",
			zap.Uint64("userID", userID),
			zap.Error(friendResult.err))
	}

	return groupResult.groups, friendResult.friends
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

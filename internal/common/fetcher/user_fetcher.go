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
	userInfoChan := make(chan *Info, len(userIDs))

	for _, userID := range userIDs {
		wg.Add(1)
		go func(id uint64) {
			defer wg.Done()

			// Fetch the user info
			userInfo, err := u.roAPI.Users().GetUserByID(context.Background(), id)
			if err != nil {
				u.logger.Warn("Error fetching user info", zap.Uint64("userID", id), zap.Error(err))
				return
			}

			// Skip banned users
			if userInfo.IsBanned {
				return
			}

			// Fetch groups and friends concurrently
			groups, friends := u.fetchGroupsAndFriends(id)

			// Send the user info to the channel
			userInfoChan <- &Info{
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

	// Close the channel when all goroutines are done
	go func() {
		wg.Wait()
		close(userInfoChan)
	}()

	// Collect results from the channel
	userInfos := make([]*Info, 0, len(userIDs))
	for userInfo := range userInfoChan {
		if userInfo != nil {
			userInfos = append(userInfos, userInfo)
		}
	}

	return userInfos
}

// fetchGroupsAndFriends retrieves a user's group memberships and friend list
// concurrently. Returns empty slices if all requests fail.
func (u *UserFetcher) fetchGroupsAndFriends(userID uint64) ([]types.UserGroupRoles, []types.Friend) {
	var wg sync.WaitGroup
	var groupRoles []types.UserGroupRoles
	var friends []types.Friend

	wg.Add(2)

	// Fetch user's groups
	go func() {
		defer wg.Done()
		builder := groups.NewUserGroupRolesBuilder(userID)
		fetchedGroups, err := u.roAPI.Groups().GetUserGroupRoles(context.Background(), builder.Build())
		if err != nil {
			u.logger.Warn("Error fetching user groups", zap.Uint64("userID", userID), zap.Error(err))
			return
		}
		groupRoles = fetchedGroups
	}()

	// Fetch user's friends
	go func() {
		defer wg.Done()
		fetchedFriends, err := u.roAPI.Friends().GetFriends(context.Background(), userID)
		if err != nil {
			u.logger.Warn("Error fetching user friends", zap.Uint64("userID", userID), zap.Error(err))
			return
		}
		friends = fetchedFriends
	}()

	wg.Wait()

	return groupRoles, friends
}

// FetchBannedUsers checks which users from a batch of IDs are currently banned.
// Returns a slice of banned user IDs.
func (u *UserFetcher) FetchBannedUsers(userIDs []uint64) ([]uint64, error) {
	var wg sync.WaitGroup
	var mu sync.Mutex
	bannedUserIDs := []uint64{}

	for _, userID := range userIDs {
		wg.Add(1)
		go func(id uint64) {
			defer wg.Done()

			// Fetch the user info
			userInfo, err := u.roAPI.Users().GetUserByID(context.Background(), id)
			if err != nil {
				u.logger.Warn("Error fetching user info", zap.Uint64("userID", id), zap.Error(err))
				return
			}

			// Save the banned user ID
			if userInfo.IsBanned {
				mu.Lock()
				bannedUserIDs = append(bannedUserIDs, userInfo.ID)
				mu.Unlock()
			}
		}(userID)
	}

	wg.Wait()

	return bannedUserIDs, nil
}

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

// Info represents the information about a user to be checked by the AI.
type Info struct {
	ID          uint64                 `json:"id"`
	Name        string                 `json:"name"`
	DisplayName string                 `json:"displayName"`
	Description string                 `json:"description"`
	CreatedAt   time.Time              `json:"createdAt"`
	Groups      []types.UserGroupRoles `json:"groupIds"`
	LastUpdated time.Time              `json:"lastUpdated"`
}

// UserFetcher handles fetching of user information.
type UserFetcher struct {
	roAPI  *api.API
	logger *zap.Logger
}

// NewUserFetcher creates a new UserFetcher instance.
func NewUserFetcher(roAPI *api.API, logger *zap.Logger) *UserFetcher {
	return &UserFetcher{
		roAPI:  roAPI,
		logger: logger,
	}
}

// FetchInfos fetches user information for a batch of user IDs.
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

			// If the user is banned, skip it
			if userInfo.IsBanned {
				return
			}

			// Fetch user's groups
			builder := groups.NewUserGroupRolesBuilder(userID)
			groups, err := u.roAPI.Groups().GetUserGroupRoles(context.Background(), builder.Build())
			if err != nil {
				u.logger.Warn("Error fetching user groups", zap.Uint64("userID", id), zap.Error(err))
				return
			}

			// Send the user info to the channel
			userInfoChan <- &Info{
				ID:          userInfo.ID,
				Name:        userInfo.Name,
				DisplayName: userInfo.DisplayName,
				Description: userInfo.Description,
				CreatedAt:   userInfo.Created,
				Groups:      groups,
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

// FetchBannedUsers fetches banned users for a batch of user IDs.
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

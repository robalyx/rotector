package fetcher

import (
	"context"
	"errors"
	"sync"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/jaxron/roapi.go/pkg/api/resources/groups"
	apiTypes "github.com/jaxron/roapi.go/pkg/api/types"
	"go.uber.org/zap"
)

// ErrGroupLocked indicates that the group is locked.
var ErrGroupLocked = errors.New("group is locked")

// GroupFetcher handles concurrent retrieval of group information from the Roblox API.
type GroupFetcher struct {
	roAPI  *api.API
	logger *zap.Logger
}

// NewGroupFetcher creates a GroupFetcher with the provided API client and logger.
func NewGroupFetcher(roAPI *api.API, logger *zap.Logger) *GroupFetcher {
	return &GroupFetcher{
		roAPI:  roAPI,
		logger: logger,
	}
}

// GroupFetchResult contains the result of fetching a group's information.
type GroupFetchResult struct {
	ID    uint64
	Info  *apiTypes.GroupResponse
	Error error
}

// FetchGroupInfos retrieves complete group information for a batch of group IDs.
func (g *GroupFetcher) FetchGroupInfos(groupIDs []uint64) []*apiTypes.GroupResponse {
	var (
		validGroups = make([]*apiTypes.GroupResponse, 0, len(groupIDs))
		mu          sync.Mutex
		wg          sync.WaitGroup
	)

	// Spawn a goroutine for each group
	for _, groupID := range groupIDs {
		wg.Add(1)
		go func(id uint64) {
			defer wg.Done()

			// Fetch the group info
			groupInfo, err := g.roAPI.Groups().GetGroupInfo(context.Background(), id)
			if err != nil {
				g.logger.Error("Error fetching group info",
					zap.Uint64("groupID", id),
					zap.Error(err))
				return
			}

			// Check for locked groups
			if groupInfo.IsLocked != nil && *groupInfo.IsLocked {
				return
			}

			mu.Lock()
			validGroups = append(validGroups, groupInfo)
			mu.Unlock()
		}(groupID)
	}

	wg.Wait()

	g.logger.Debug("Finished fetching group information",
		zap.Int("totalRequested", len(groupIDs)),
		zap.Int("successfulFetches", len(validGroups)))

	return validGroups
}

// FetchLockedGroups checks which groups from a batch of IDs are currently locked.
// Returns a slice of locked group IDs.
func (g *GroupFetcher) FetchLockedGroups(groupIDs []uint64) ([]uint64, error) {
	var (
		results = make([]uint64, 0, len(groupIDs))
		mu      sync.Mutex
		wg      sync.WaitGroup
	)

	for _, groupID := range groupIDs {
		wg.Add(1)
		go func(id uint64) {
			defer wg.Done()

			groupInfo, err := g.roAPI.Groups().GetGroupInfo(context.Background(), id)
			if err != nil {
				g.logger.Error("Error fetching group info",
					zap.Uint64("groupID", id),
					zap.Error(err))
				return
			}

			if groupInfo.IsLocked != nil && *groupInfo.IsLocked {
				mu.Lock()
				results = append(results, groupInfo.ID)
				mu.Unlock()
			}
		}(groupID)
	}

	wg.Wait()

	g.logger.Debug("Finished checking locked groups",
		zap.Int("totalChecked", len(groupIDs)),
		zap.Int("lockedGroups", len(results)))

	return results, nil
}

// GetUserGroups retrieves all groups for a user.
func (g *GroupFetcher) GetUserGroups(ctx context.Context, userID uint64) ([]*apiTypes.UserGroupRoles, error) {
	builder := groups.NewUserGroupRolesBuilder(userID)
	fetchedGroups, err := g.roAPI.Groups().GetUserGroupRoles(ctx, builder.Build())
	if err != nil {
		return nil, err
	}

	groups := make([]*apiTypes.UserGroupRoles, 0, len(fetchedGroups.Data))
	for _, group := range fetchedGroups.Data {
		groups = append(groups, &group)
	}

	g.logger.Debug("Finished fetching user groups",
		zap.Uint64("userID", userID),
		zap.Int("totalGroups", len(groups)))

	return groups, nil
}

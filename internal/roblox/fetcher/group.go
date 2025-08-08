package fetcher

import (
	"context"
	"sync"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/jaxron/roapi.go/pkg/api/middleware/auth"
	"github.com/jaxron/roapi.go/pkg/api/resources/groups"
	apiTypes "github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/robalyx/rotector/pkg/utils"
	"github.com/sourcegraph/conc/pool"
	"go.uber.org/zap"
)

// GroupFetcher handles concurrent retrieval of group information from the Roblox API.
type GroupFetcher struct {
	roAPI  *api.API
	logger *zap.Logger
}

// NewGroupFetcher creates a GroupFetcher with the provided API client and logger.
func NewGroupFetcher(roAPI *api.API, logger *zap.Logger) *GroupFetcher {
	return &GroupFetcher{
		roAPI:  roAPI,
		logger: logger.Named("group_fetcher"),
	}
}

// GroupFetchResult contains the result of fetching a group's information.
type GroupFetchResult struct {
	ID    uint64
	Info  *apiTypes.GroupResponse
	Error error
}

// FetchGroupInfos retrieves complete group information for a batch of group IDs.
func (g *GroupFetcher) FetchGroupInfos(ctx context.Context, groupIDs []uint64) []*apiTypes.GroupResponse {
	ctx = context.WithValue(ctx, auth.KeyAddCookie, true)

	var (
		validGroups = make([]*apiTypes.GroupResponse, 0, len(groupIDs))
		p           = pool.New().WithContext(ctx)
		mu          sync.Mutex
	)

	// Process each group concurrently
	for _, id := range groupIDs {
		p.Go(func(ctx context.Context) error {
			// Fetch the group info
			groupInfo, err := g.roAPI.Groups().GetGroupInfo(ctx, id)
			if err != nil {
				g.logger.Error("Error fetching group info",
					zap.Uint64("groupID", id),
					zap.Error(err))

				return nil // Don't fail the whole batch for one error
			}

			// Check for locked groups
			if groupInfo.IsLocked != nil && *groupInfo.IsLocked {
				return nil
			}

			// Normalize text fields
			normalizer := utils.NewTextNormalizer()

			groupInfo.Name = normalizer.Normalize(groupInfo.Name)

			groupInfo.Description = normalizer.Normalize(groupInfo.Description)
			if groupInfo.Owner != nil {
				groupInfo.Owner.DisplayName = normalizer.Normalize(groupInfo.Owner.DisplayName)
			}

			if groupInfo.Shout != nil {
				groupInfo.Shout.Body = normalizer.Normalize(groupInfo.Shout.Body)
				groupInfo.Shout.Poster.DisplayName = normalizer.Normalize(groupInfo.Shout.Poster.DisplayName)
			}

			mu.Lock()

			validGroups = append(validGroups, groupInfo)

			mu.Unlock()

			return nil
		})
	}

	// Wait for all goroutines to complete
	if err := p.Wait(); err != nil {
		g.logger.Error("Error during group fetch", zap.Error(err))
	}

	g.logger.Debug("Finished fetching group information",
		zap.Int("totalRequested", len(groupIDs)),
		zap.Int("successfulFetches", len(validGroups)))

	return validGroups
}

// FetchLockedGroups checks which groups from a batch of IDs are currently locked.
// Returns a slice of locked group IDs.
func (g *GroupFetcher) FetchLockedGroups(ctx context.Context, groupIDs []uint64) ([]uint64, error) {
	ctx = context.WithValue(ctx, auth.KeyAddCookie, true)

	var (
		results = make([]uint64, 0, len(groupIDs))
		p       = pool.New().WithContext(ctx)
		mu      sync.Mutex
	)

	for _, id := range groupIDs {
		p.Go(func(ctx context.Context) error {
			groupInfo, err := g.roAPI.Groups().GetGroupInfo(ctx, id)
			if err != nil {
				g.logger.Error("Error fetching group info",
					zap.Uint64("groupID", id),
					zap.Error(err))

				return nil // Don't fail the whole batch for one error
			}

			if groupInfo.IsLocked != nil && *groupInfo.IsLocked {
				mu.Lock()

				results = append(results, groupInfo.ID)

				mu.Unlock()
			}

			return nil
		})
	}

	// Wait for all goroutines to complete
	if err := p.Wait(); err != nil {
		g.logger.Error("Error during locked groups fetch", zap.Error(err))
		return nil, err
	}

	g.logger.Debug("Finished checking locked groups",
		zap.Int("totalChecked", len(groupIDs)),
		zap.Int("lockedGroups", len(results)))

	return results, nil
}

// GetUserGroups retrieves all groups for a user.
func (g *GroupFetcher) GetUserGroups(ctx context.Context, userID uint64) ([]*apiTypes.UserGroupRoles, error) {
	ctx = context.WithValue(ctx, auth.KeyAddCookie, true)
	builder := groups.NewUserGroupRolesBuilder(userID)

	fetchedGroups, err := g.roAPI.Groups().GetUserGroupRoles(ctx, builder.Build())
	if err != nil {
		return nil, err
	}

	groupsData := make([]*apiTypes.UserGroupRoles, 0, len(fetchedGroups.Data))
	for _, group := range fetchedGroups.Data {
		groupsData = append(groupsData, &group)
	}

	g.logger.Debug("Finished fetching user groups",
		zap.Uint64("userID", userID),
		zap.Int("totalGroups", len(groupsData)))

	return groupsData, nil
}

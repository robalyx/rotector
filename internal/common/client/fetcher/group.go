package fetcher

import (
	"context"
	"errors"
	"sync"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/jaxron/roapi.go/pkg/api/types"
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
	GroupInfo *types.GroupResponse
	Error     error
}

// FetchGroupInfos retrieves complete group information for a batch of group IDs.
func (g *GroupFetcher) FetchGroupInfos(groupIDs []uint64) []*types.GroupResponse {
	var wg sync.WaitGroup
	resultsChan := make(chan struct {
		GroupID uint64
		Result  *GroupFetchResult
	}, len(groupIDs))

	// Spawn a goroutine for each group
	for _, groupID := range groupIDs {
		wg.Add(1)
		go func(id uint64) {
			defer wg.Done()

			// Fetch the group info
			groupInfo, err := g.roAPI.Groups().GetGroupInfo(context.Background(), id)
			if err != nil {
				resultsChan <- struct {
					GroupID uint64
					Result  *GroupFetchResult
				}{
					GroupID: id,
					Result: &GroupFetchResult{
						Error: err,
					},
				}
				return
			}

			// Check for locked groups
			if groupInfo.IsLocked != nil && *groupInfo.IsLocked {
				resultsChan <- struct {
					GroupID uint64
					Result  *GroupFetchResult
				}{
					GroupID: id,
					Result: &GroupFetchResult{
						Error: ErrGroupLocked,
					},
				}
				return
			}

			resultsChan <- struct {
				GroupID uint64
				Result  *GroupFetchResult
			}{
				GroupID: id,
				Result: &GroupFetchResult{
					GroupInfo: groupInfo,
				},
			}
		}(groupID)
	}

	// Close the channel when all goroutines are done
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// Collect results from the channel
	results := make(map[uint64]*GroupFetchResult)
	for result := range resultsChan {
		results[result.GroupID] = result.Result
	}

	// Process results and filter out errors
	validGroups := make([]*types.GroupResponse, 0, len(results))
	for groupID, result := range results {
		if result.Error != nil {
			if !errors.Is(result.Error, ErrGroupLocked) {
				g.logger.Warn("Error fetching group info",
					zap.Uint64("groupID", groupID),
					zap.Error(result.Error))
			}
			continue
		}

		validGroups = append(validGroups, result.GroupInfo)
	}

	g.logger.Debug("Finished fetching group information",
		zap.Int("totalRequested", len(groupIDs)),
		zap.Int("successfulFetches", len(validGroups)))

	return validGroups
}

// FetchLockedGroups checks which groups from a batch of IDs are currently locked.
// Returns a slice of locked group IDs.
func (g *GroupFetcher) FetchLockedGroups(groupIDs []uint64) ([]uint64, error) {
	var wg sync.WaitGroup
	results := make([]uint64, 0, len(groupIDs))
	groupChan := make(chan uint64, len(groupIDs))

	for _, groupID := range groupIDs {
		wg.Add(1)
		go func(id uint64) {
			defer wg.Done()

			groupInfo, err := g.roAPI.Groups().GetGroupInfo(context.Background(), id)
			if err != nil {
				g.logger.Warn("Error fetching group info",
					zap.Uint64("groupID", id),
					zap.Error(err))
				return
			}

			if groupInfo.IsLocked != nil && *groupInfo.IsLocked {
				groupChan <- groupInfo.ID
			}
		}(groupID)
	}

	go func() {
		wg.Wait()
		close(groupChan)
	}()

	for id := range groupChan {
		results = append(results, id)
	}

	g.logger.Debug("Finished checking locked groups",
		zap.Int("totalChecked", len(groupIDs)),
		zap.Int("lockedGroups", len(results)))

	return results, nil
}

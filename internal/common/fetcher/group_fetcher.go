package fetcher

import (
	"context"
	"sync"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/jaxron/roapi.go/pkg/api/types"
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
		logger: logger,
	}
}

// FetchGroupInfos retrieves complete group information for a batch of group IDs.
// It spawns a goroutine for each group and collects results through a channel.
// Failed requests are logged and skipped in the final results.
func (g *GroupFetcher) FetchGroupInfos(groupIDs []uint64) []*types.GroupResponse {
	var wg sync.WaitGroup
	groupInfoChan := make(chan *types.GroupResponse, len(groupIDs))
	groupInfos := make([]*types.GroupResponse, 0, len(groupIDs))

	for _, groupID := range groupIDs {
		wg.Add(1)
		go func(id uint64) {
			defer wg.Done()

			// Fetch the group info
			groupInfo, err := g.roAPI.Groups().GetGroupInfo(context.Background(), id)
			if err != nil {
				g.logger.Warn("Error fetching group info",
					zap.Uint64("groupID", id),
					zap.Error(err))
				return
			}

			// Skip locked groups
			if groupInfo.IsLocked != nil && *groupInfo.IsLocked {
				return
			}

			// Send the group info to the channel
			groupInfoChan <- groupInfo
		}(groupID)
	}

	// Close the channel when all goroutines are done
	go func() {
		wg.Wait()
		close(groupInfoChan)
	}()

	// Collect results from the channel
	for groupInfo := range groupInfoChan {
		groupInfos = append(groupInfos, groupInfo)
	}

	g.logger.Info("Finished fetching group information",
		zap.Int("totalRequested", len(groupIDs)),
		zap.Int("successfulFetches", len(groupInfos)))

	return groupInfos
}

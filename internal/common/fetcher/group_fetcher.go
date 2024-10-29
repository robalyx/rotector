package fetcher

import (
	"context"
	"sync"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/jaxron/roapi.go/pkg/api/types"
	"go.uber.org/zap"
)

// GroupFetcher handles fetching of group information.
type GroupFetcher struct {
	roAPI  *api.API
	logger *zap.Logger
}

// NewGroupFetcher creates a new GroupFetcher instance.
func NewGroupFetcher(roAPI *api.API, logger *zap.Logger) *GroupFetcher {
	return &GroupFetcher{
		roAPI:  roAPI,
		logger: logger,
	}
}

// FetchGroupInfos fetches group information for a batch of group IDs.
func (g *GroupFetcher) FetchGroupInfos(groupIDs []uint64) []*types.GroupResponse {
	var wg sync.WaitGroup
	groupInfoChan := make(chan *types.GroupResponse, len(groupIDs))

	for _, groupID := range groupIDs {
		wg.Add(1)
		go func(id uint64) {
			defer wg.Done()

			// Fetch the group info
			groupInfo, err := g.roAPI.Groups().GetGroupInfo(context.Background(), id)
			if err != nil {
				g.logger.Warn("Error fetching group info", zap.Uint64("groupID", id), zap.Error(err))
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
	groupInfos := make([]*types.GroupResponse, 0, len(groupIDs))
	for groupInfo := range groupInfoChan {
		if groupInfo != nil {
			groupInfos = append(groupInfos, groupInfo)
		}
	}

	return groupInfos
}

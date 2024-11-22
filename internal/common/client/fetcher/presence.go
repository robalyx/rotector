package fetcher

import (
	"context"
	"sync"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/jaxron/roapi.go/pkg/api/resources/presence"
	"github.com/jaxron/roapi.go/pkg/api/types"
	"go.uber.org/zap"
)

// PresenceFetchResult contains the result of fetching user presences.
type PresenceFetchResult struct {
	Presences *types.UserPresencesResponse
	Error     error
}

// PresenceFetcher handles retrieval of user presence information from the Roblox API.
type PresenceFetcher struct {
	roAPI  *api.API
	logger *zap.Logger
}

// NewPresenceFetcher creates a PresenceFetcher.
func NewPresenceFetcher(roAPI *api.API, logger *zap.Logger) *PresenceFetcher {
	return &PresenceFetcher{
		roAPI:  roAPI,
		logger: logger,
	}
}

// FetchPresences retrieves presence information for a batch of user IDs.
// It processes users in batches of 50 (Roblox API limit) and returns all successful presence fetches.
func (p *PresenceFetcher) FetchPresences(userIDs []uint64) []*types.UserPresenceResponse {
	var wg sync.WaitGroup
	batchSize := 50
	numBatches := (len(userIDs) + batchSize - 1) / batchSize
	resultsChan := make(chan *PresenceFetchResult, numBatches)

	// Process users in batches of 50 (Roblox API limit)
	for i := 0; i < len(userIDs); i += batchSize {
		wg.Add(1)
		go func(start int) {
			defer wg.Done()

			// Get the current batch of user IDs
			end := start + batchSize
			if end > len(userIDs) {
				end = len(userIDs)
			}
			batchIDs := userIDs[start:end]

			// Fetch presences for the batch
			params := presence.NewUserPresencesBuilder(batchIDs...).Build()
			presences, err := p.roAPI.Presence().GetUserPresences(context.Background(), params)
			resultsChan <- &PresenceFetchResult{
				Presences: presences,
				Error:     err,
			}
		}(i)
	}

	// Close channel when all goroutines complete
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// Pre-allocate the slice with estimated capacity
	allPresences := make([]*types.UserPresenceResponse, 0, len(userIDs))

	// Collect and combine results from all batches
	for result := range resultsChan {
		if result.Error != nil {
			p.logger.Warn("Error fetching user presences",
				zap.Error(result.Error))
			continue
		}

		for _, presence := range result.Presences.UserPresences {
			allPresences = append(allPresences, &presence)
		}
	}

	p.logger.Debug("Finished fetching user presences",
		zap.Int("totalRequested", len(userIDs)),
		zap.Int("successfulFetches", len(allPresences)))

	return allPresences
}

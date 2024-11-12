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
	Presences []types.UserPresence
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
func (p *PresenceFetcher) FetchPresences(userIDs []uint64) []types.UserPresence {
	var wg sync.WaitGroup
	resultsChan := make(chan *PresenceFetchResult, (len(userIDs)+99)/100) // Ceiling division for batch count

	// Process users in batches of 50 (Roblox API limit)
	batchSize := 50
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

	// Collect and combine results from all batches
	var allPresences []types.UserPresence
	successfulFetches := 0

	for result := range resultsChan {
		if result.Error != nil {
			p.logger.Warn("Error fetching user presences",
				zap.Error(result.Error))
			continue
		}

		allPresences = append(allPresences, result.Presences...)
		successfulFetches += len(result.Presences)
	}

	p.logger.Info("Finished fetching user presences",
		zap.Int("totalRequested", len(userIDs)),
		zap.Int("successfulFetches", successfulFetches))

	return allPresences
}

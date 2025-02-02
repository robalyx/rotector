package fetcher

import (
	"context"
	"sync"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/jaxron/roapi.go/pkg/api/middleware/auth"
	"github.com/jaxron/roapi.go/pkg/api/resources/presence"
	"github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/sourcegraph/conc/pool"
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
func (p *PresenceFetcher) FetchPresences(ctx context.Context, userIDs []uint64) []*types.UserPresenceResponse {
	ctx = context.WithValue(ctx, auth.KeyAddCookie, true)

	var (
		allPresences = make([]*types.UserPresenceResponse, 0, len(userIDs))
		pool         = pool.New().WithContext(ctx)
		mu           sync.Mutex
		batchSize    = 50
	)

	// Process batches concurrently
	for i := 0; i < len(userIDs); i += batchSize {
		pool.Go(func(ctx context.Context) error {
			end := i + batchSize
			if end > len(userIDs) {
				end = len(userIDs)
			}

			// Fetch presences for the batch
			params := presence.NewUserPresencesBuilder(userIDs[i:end]...).Build()
			presences, err := p.roAPI.Presence().GetUserPresences(ctx, params)
			if err != nil {
				p.logger.Error("Error fetching user presences",
					zap.Error(err),
					zap.Int("batchStart", i))
				return nil // Don't fail the whole batch for one error
			}

			mu.Lock()
			for _, presence := range presences.UserPresences {
				allPresences = append(allPresences, &presence)
			}
			mu.Unlock()

			return nil
		})
	}

	// Wait for all goroutines to complete
	if err := pool.Wait(); err != nil {
		p.logger.Error("Error during presence fetch", zap.Error(err))
	}

	p.logger.Debug("Finished fetching user presences",
		zap.Int("totalRequested", len(userIDs)),
		zap.Int("successfulFetches", len(allPresences)))

	return allPresences
}

// FetchPresencesConcurrently fetches presences in the background and returns a channel with the results.
func (p *PresenceFetcher) FetchPresencesConcurrently(ctx context.Context, userIDs []uint64) <-chan map[uint64]*types.UserPresenceResponse {
	resultChan := make(chan map[uint64]*types.UserPresenceResponse, 1)

	go func() {
		presences := p.FetchPresences(ctx, userIDs)
		presenceMap := make(map[uint64]*types.UserPresenceResponse)
		for _, presence := range presences {
			presenceMap[presence.UserID] = presence
		}
		resultChan <- presenceMap
		close(resultChan)
	}()

	return resultChan
}

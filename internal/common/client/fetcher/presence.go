package fetcher

import (
	"context"
	"sync"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/jaxron/roapi.go/pkg/api/middleware/auth"
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
func (p *PresenceFetcher) FetchPresences(ctx context.Context, userIDs []uint64) []*types.UserPresenceResponse {
	ctx = context.WithValue(ctx, auth.KeyAddCookie, true)

	var (
		allPresences = make([]*types.UserPresenceResponse, 0, len(userIDs))
		mu           sync.Mutex
		wg           sync.WaitGroup
		batchSize    = 50
	)

	for i := 0; i < len(userIDs); i += batchSize {
		wg.Add(1)
		go func(start, end int) {
			defer wg.Done()

			// Fetch presences for the batch
			params := presence.NewUserPresencesBuilder(userIDs[start:end]...).Build()
			presences, err := p.roAPI.Presence().GetUserPresences(ctx, params)
			if err != nil {
				p.logger.Error("Error fetching user presences",
					zap.Error(err),
					zap.Int("batchStart", start))
				return
			}

			mu.Lock()
			for _, presence := range presences.UserPresences {
				allPresences = append(allPresences, &presence)
			}
			mu.Unlock()
		}(i, minInt(i+batchSize, len(userIDs)))
	}

	wg.Wait()

	p.logger.Debug("Finished fetching user presences",
		zap.Int("totalRequested", len(userIDs)),
		zap.Int("successfulFetches", len(allPresences)))

	return allPresences
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

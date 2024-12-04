package fetcher

import (
	"context"
	"sync"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/jaxron/roapi.go/pkg/api/resources/avatar"
	apiTypes "github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/rotector/rotector/internal/common/storage/database/types"
	"go.uber.org/zap"
)

// OutfitFetchResult contains the result of fetching a user's outfits.
type OutfitFetchResult struct {
	ID      uint64
	Outfits *apiTypes.OutfitResponse
	Error   error
}

// OutfitFetcher handles retrieval of user outfit information from the Roblox API.
type OutfitFetcher struct {
	roAPI  *api.API
	logger *zap.Logger
}

// NewOutfitFetcher creates an OutfitFetcher with the provided API client and logger.
func NewOutfitFetcher(roAPI *api.API, logger *zap.Logger) *OutfitFetcher {
	return &OutfitFetcher{
		roAPI:  roAPI,
		logger: logger,
	}
}

// AddOutfits fetches outfits for a batch of users and returns a map of results.
func (o *OutfitFetcher) AddOutfits(users map[uint64]*types.User) map[uint64]*OutfitFetchResult {
	var wg sync.WaitGroup
	resultsChan := make(chan OutfitFetchResult, len(users))

	// Process each user concurrently
	for _, user := range users {
		wg.Add(1)
		go func(u *types.User) {
			defer wg.Done()

			builder := avatar.NewUserOutfitsBuilder(u.ID).WithItemsPerPage(1000).WithIsEditable(true)
			outfits, err := o.roAPI.Avatar().GetUserOutfits(context.Background(), builder.Build())
			resultsChan <- OutfitFetchResult{
				ID:      u.ID,
				Outfits: outfits,
				Error:   err,
			}
		}(user)
	}

	// Close channel when all goroutines complete
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// Collect results from the channel
	results := make(map[uint64]*OutfitFetchResult, len(users))
	for result := range resultsChan {
		if result.Error != nil {
			o.logger.Error("Failed to fetch user outfits",
				zap.Error(result.Error),
				zap.Uint64("userID", result.ID))
			continue
		}
		results[result.ID] = &result
	}

	o.logger.Debug("Finished fetching user outfits",
		zap.Int("totalUsers", len(users)),
		zap.Int("successfulFetches", len(results)))

	return results
}

package fetcher

import (
	"context"
	"sync"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/jaxron/roapi.go/pkg/api/resources/avatar"
	"github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/rotector/rotector/internal/common/storage/database/models"
	"go.uber.org/zap"
)

// OutfitFetchResult contains the result of fetching a user's outfits.
type OutfitFetchResult struct {
	ID      uint64
	Outfits *types.OutfitResponse
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

// AddOutfits fetches outfits for a batch of users and adds them to the user records.
func (o *OutfitFetcher) AddOutfits(users map[uint64]*models.User) map[uint64]*models.User {
	var wg sync.WaitGroup
	resultsChan := make(chan OutfitFetchResult, len(users))

	// Process each user concurrently
	for _, user := range users {
		wg.Add(1)
		go func(u *models.User) {
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
	results := make(map[uint64]*OutfitFetchResult)
	for result := range resultsChan {
		results[result.ID] = &result
	}

	// Process results and maintain original order
	updatedUsers := make(map[uint64]*models.User, len(users))
	for _, user := range users {
		result := results[user.ID]
		if result.Error != nil {
			o.logger.Error("Failed to fetch user outfits",
				zap.Error(result.Error),
				zap.Uint64("userID", user.ID))
			continue
		}
		user.Outfits = result.Outfits.Data
		updatedUsers[user.ID] = user
	}

	o.logger.Debug("Finished fetching user outfits",
		zap.Int("totalUsers", len(users)),
		zap.Int("successfulFetches", len(updatedUsers)))

	return updatedUsers
}

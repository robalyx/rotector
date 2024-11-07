package fetcher

import (
	"context"
	"sync"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/jaxron/roapi.go/pkg/api/resources/avatar"
	"github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/rotector/rotector/internal/common/database"
	"go.uber.org/zap"
)

// OutfitFetchResult contains the result of fetching a user's outfits.
type OutfitFetchResult struct {
	Outfits []types.Outfit
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
func (o *OutfitFetcher) AddOutfits(users []*database.User) []*database.User {
	var wg sync.WaitGroup
	resultsChan := make(chan struct {
		UserID uint64
		Result *OutfitFetchResult
	}, len(users))

	// Create a map to maintain order of users
	userMap := make(map[uint64]*database.User)
	for _, user := range users {
		userMap[user.ID] = user
	}

	// Process each user concurrently
	for _, user := range users {
		wg.Add(1)
		go func(u *database.User) {
			defer wg.Done()

			builder := avatar.NewUserOutfitsBuilder(u.ID).WithItemsPerPage(1000).WithIsEditable(true)
			outfits, err := o.roAPI.Avatar().GetUserOutfits(context.Background(), builder.Build())
			resultsChan <- struct {
				UserID uint64
				Result *OutfitFetchResult
			}{
				UserID: u.ID,
				Result: &OutfitFetchResult{
					Outfits: outfits,
					Error:   err,
				},
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
		results[result.UserID] = result.Result
	}

	// Process results and maintain original order
	updatedUsers := make([]*database.User, 0, len(users))
	for _, user := range users {
		result := results[user.ID]
		if result.Error != nil {
			o.logger.Error("Failed to fetch user outfits",
				zap.Error(result.Error),
				zap.Uint64("userID", user.ID))
			continue
		}
		user.Outfits = result.Outfits
		updatedUsers = append(updatedUsers, user)
	}

	o.logger.Info("Finished fetching user outfits",
		zap.Int("totalUsers", len(users)),
		zap.Int("successfulFetches", len(updatedUsers)))

	return updatedUsers
}

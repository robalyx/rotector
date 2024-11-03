package fetcher

import (
	"context"
	"sync"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/jaxron/roapi.go/pkg/api/resources/avatar"
	"github.com/rotector/rotector/internal/common/database"
	"go.uber.org/zap"
)

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
	userChan := make(chan *database.User, len(users))

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
			if err != nil {
				o.logger.Error("Failed to fetch user outfits",
					zap.Error(err),
					zap.Uint64("userID", u.ID))
				return
			}

			// Update the outfits directly on the user pointer
			u.Outfits = outfits
			userChan <- u
		}(user)
	}

	// Close channel when all goroutines complete
	go func() {
		wg.Wait()
		close(userChan)
	}()

	// Collect results and maintain original order
	updatedUsers := make([]*database.User, 0, len(users))
	for user := range userChan {
		if user != nil {
			userMap[user.ID] = user
		}
	}

	// Reconstruct slice in original order
	for _, user := range users {
		if updatedUser, ok := userMap[user.ID]; ok {
			updatedUsers = append(updatedUsers, updatedUser)
		}
	}

	o.logger.Info("Finished fetching user outfits",
		zap.Int("totalUsers", len(users)),
		zap.Int("successfulFetches", len(updatedUsers)))

	return updatedUsers
}

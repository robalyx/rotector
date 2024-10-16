package fetcher

import (
	"context"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/jaxron/roapi.go/pkg/api/resources/avatar"
	"github.com/rotector/rotector/internal/common/database"
	"go.uber.org/zap"
)

// OutfitFetcher handles fetching of user outfits.
type OutfitFetcher struct {
	roAPI  *api.API
	logger *zap.Logger
}

// NewOutfitFetcher creates a new OutfitFetcher instance.
func NewOutfitFetcher(roAPI *api.API, logger *zap.Logger) *OutfitFetcher {
	return &OutfitFetcher{
		roAPI:  roAPI,
		logger: logger,
	}
}

// AddOutfits fetches outfits for a batch of users and adds them to the users.
func (o *OutfitFetcher) AddOutfits(users []database.User) []database.User {
	for i, user := range users {
		builder := avatar.NewUserOutfitsBuilder(user.ID).WithItemsPerPage(1000).WithIsEditable(true)
		outfits, err := o.roAPI.Avatar().GetUserOutfits(context.Background(), builder.Build())
		if err != nil {
			o.logger.Error("Failed to fetch user outfits", zap.Error(err), zap.Uint64("userID", user.ID))
			continue
		}

		users[i].Outfits = outfits
	}

	o.logger.Info("Finished fetching user outfits", zap.Int("totalUsers", len(users)))

	return users
}

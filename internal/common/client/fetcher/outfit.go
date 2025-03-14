package fetcher

import (
	"context"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/jaxron/roapi.go/pkg/api/middleware/auth"
	"github.com/jaxron/roapi.go/pkg/api/resources/avatar"
	apiTypes "github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/robalyx/rotector/internal/common/utils"
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
		logger: logger.Named("outfit_fetcher"),
	}
}

// GetOutfits fetches outfits for a single user.
func (o *OutfitFetcher) GetOutfits(ctx context.Context, userID uint64) (*apiTypes.OutfitResponse, error) {
	ctx = context.WithValue(ctx, auth.KeyAddCookie, true)
	builder := avatar.NewUserOutfitsBuilder(userID).WithItemsPerPage(100).WithIsEditable(true)

	outfits, err := o.roAPI.Avatar().GetUserOutfits(ctx, builder.Build())
	if err != nil {
		o.logger.Error("Failed to fetch user outfits",
			zap.Error(err),
			zap.Uint64("userID", userID))
		return nil, err
	}

	// Normalize outfit names
	normalizer := utils.NewTextNormalizer()
	for i := range outfits.Data {
		outfits.Data[i].Name = normalizer.Normalize(outfits.Data[i].Name)
	}

	o.logger.Debug("Successfully fetched user outfits",
		zap.Uint64("userID", userID),
		zap.Int("outfitCount", len(outfits.Data)))

	return outfits, nil
}

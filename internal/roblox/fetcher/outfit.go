package fetcher

import (
	"context"
	"sync"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/jaxron/roapi.go/pkg/api/middleware/auth"
	"github.com/jaxron/roapi.go/pkg/api/resources/avatar"
	apiTypes "github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/robalyx/rotector/pkg/utils"
	"github.com/sourcegraph/conc/pool"
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

// GetOutfits fetches basic outfit information and current avatar assets for a user.
func (o *OutfitFetcher) GetOutfits(
	ctx context.Context, userID uint64,
) ([]*apiTypes.Outfit, []*apiTypes.AssetV2, error) {
	ctx = context.WithValue(ctx, auth.KeyAddCookie, true)

	// Get the user's current avatar details
	avatarDetails, err := o.roAPI.Avatar().GetUserAvatar(ctx, userID)
	if err != nil {
		o.logger.Error("Failed to fetch user avatar",
			zap.Error(err),
			zap.Uint64("userID", userID))

		return nil, nil, err
	}

	// Get user's outfits
	builder := avatar.NewUserOutfitsBuilder(userID).WithItemsPerPage(100).WithIsEditable(true)

	outfits, err := o.roAPI.Avatar().GetUserOutfits(ctx, builder.Build())
	if err != nil {
		o.logger.Error("Failed to fetch user outfits",
			zap.Error(err),
			zap.Uint64("userID", userID))

		return nil, nil, err
	}

	// Normalize outfit names
	normalizer := utils.NewTextNormalizer()
	for i := range outfits.Data {
		outfits.Data[i].Name = normalizer.Normalize(outfits.Data[i].Name)
	}

	o.logger.Debug("Successfully fetched user outfits",
		zap.Uint64("userID", userID),
		zap.Int("outfitCount", len(outfits.Data)))

	return outfits.Data, avatarDetails.Assets, nil
}

// GetOutfitDetails fetches detailed asset information for specific outfits.
func (o *OutfitFetcher) GetOutfitDetails(
	ctx context.Context, outfits []*apiTypes.Outfit,
) (map[uint64][]*apiTypes.AssetV2, error) {
	ctx = context.WithValue(ctx, auth.KeyAddCookie, true)

	var (
		outfitAssets = make(map[uint64][]*apiTypes.AssetV2)
		p            = pool.New().WithContext(ctx).WithMaxGoroutines(5)
		mu           sync.Mutex
	)

	for _, outfit := range outfits {
		outfitID := outfit.ID

		p.Go(func(ctx context.Context) error {
			details, err := o.roAPI.Avatar().GetOutfitDetails(ctx, outfitID)
			if err != nil {
				o.logger.Warn("Failed to fetch outfit details",
					zap.Error(err),
					zap.Uint64("outfitID", outfitID))

				return nil
			}

			mu.Lock()

			outfitAssets[outfitID] = details.Assets

			mu.Unlock()

			return nil
		})
	}

	// Wait for all outfit detail fetches to complete
	_ = p.Wait()

	o.logger.Debug("Successfully fetched outfit details",
		zap.Int("outfitCount", len(outfits)),
		zap.Int("outfitsWithAssets", len(outfitAssets)))

	return outfitAssets, nil
}

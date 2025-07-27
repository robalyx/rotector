package fetcher

import (
	"context"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/jaxron/roapi.go/pkg/api/resources/inventory"
	"github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/robalyx/rotector/pkg/utils"
	"go.uber.org/zap"
)

// InventoryFetcher handles retrieval of user inventory information from the Roblox API.
type InventoryFetcher struct {
	roAPI  *api.API
	logger *zap.Logger
}

// NewInventoryFetcher creates an InventoryFetcher with the provided API client and logger.
func NewInventoryFetcher(roAPI *api.API, logger *zap.Logger) *InventoryFetcher {
	return &InventoryFetcher{
		roAPI:  roAPI,
		logger: logger.Named("inventory_fetcher"),
	}
}

// GetInventory retrieves a user's inventory items of specified types.
func (i *InventoryFetcher) GetInventory(ctx context.Context, userID uint64) ([]*types.InventoryAsset, error) {
	// Define the asset types we want to fetch
	assetTypes := []types.ItemAssetType{
		types.ItemAssetTypeTShirt,
		types.ItemAssetTypeHat,
		types.ItemAssetTypeShirt,
		types.ItemAssetTypePants,
		types.ItemAssetTypeDecal,
	}

	var (
		allAssets  = make([]*types.InventoryAsset, 0, 100)
		cursor     string
		normalizer = utils.NewTextNormalizer()
	)

	for {
		// Create request builder
		builder := inventory.NewGetUserAssetsBuilder(userID, assetTypes...).
			WithLimit(100).
			WithSortOrder(types.SortOrderDesc)

		if cursor != "" {
			builder.WithCursor(cursor)
		}

		// Fetch page of inventory assets
		response, err := i.roAPI.Inventory().GetUserAssets(ctx, builder.Build())
		if err != nil {
			return nil, err
		}

		// Process assets from this page
		for _, asset := range response.Data {
			asset.Name = normalizer.Normalize(asset.Name)
			allAssets = append(allAssets, &asset)
		}

		// Check if there are more pages
		if response.NextPageCursor == nil || *response.NextPageCursor == "" {
			break
		}

		cursor = *response.NextPageCursor
	}

	i.logger.Debug("Successfully fetched user inventory",
		zap.Uint64("userID", userID),
		zap.Int("totalAssets", len(allAssets)))

	return allAssets, nil
}

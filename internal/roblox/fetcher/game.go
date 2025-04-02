package fetcher

import (
	"context"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/jaxron/roapi.go/pkg/api/middleware/auth"
	"github.com/jaxron/roapi.go/pkg/api/resources/games"
	"github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/robalyx/rotector/pkg/utils"
	"go.uber.org/zap"
)

// GameDetails contains the result of fetching a user's games.
type GameDetails struct {
	Data []types.Game
}

// GameFetcher handles retrieval of user game information from the Roblox API.
type GameFetcher struct {
	roAPI  *api.API
	logger *zap.Logger
}

// NewGameFetcher creates a GameFetcher with the provided API client and logger.
func NewGameFetcher(roAPI *api.API, logger *zap.Logger) *GameFetcher {
	return &GameFetcher{
		roAPI:  roAPI,
		logger: logger.Named("game_fetcher"),
	}
}

// FetchGamesForUser retrieves all games for a single user.
func (g *GameFetcher) FetchGamesForUser(ctx context.Context, userID uint64) ([]*types.Game, error) {
	ctx = context.WithValue(ctx, auth.KeyAddCookie, true)

	var (
		allGames   = make([]*types.Game, 0, 50)
		cursor     string
		normalizer = utils.NewTextNormalizer()
	)

	for {
		// Create request builder
		builder := games.NewUserGamesBuilder(userID).
			WithLimit(50).
			WithSortOrder(types.SortOrderDesc)

		// Add cursor if we're not on the first page
		if cursor != "" {
			builder.WithCursor(cursor)
		}

		// Fetch page of games
		response, err := g.roAPI.Games().GetUserGames(ctx, builder.Build())
		if err != nil {
			return nil, err
		}

		// Append games from this page
		for _, game := range response.Data {
			normalizedGame := game
			normalizedGame.Name = normalizer.Normalize(game.Name)
			normalizedGame.Description = normalizer.Normalize(game.Description)
			allGames = append(allGames, &normalizedGame)
		}

		// Check if there are more pages
		if response.NextPageCursor == nil || *response.NextPageCursor == "" {
			break
		}

		cursor = *response.NextPageCursor
	}

	g.logger.Debug("Finished fetching games",
		zap.Uint64("userID", userID),
		zap.Int("totalGames", len(allGames)))

	return allGames, nil
}

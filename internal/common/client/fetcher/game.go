package fetcher

import (
	"context"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/jaxron/roapi.go/pkg/api/resources/games"
	"github.com/jaxron/roapi.go/pkg/api/types"
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
		logger: logger,
	}
}

// fetchGamesForUser retrieves all games for a single user.
func (g *GameFetcher) FetchGamesForUser(userID uint64) ([]*types.Game, error) {
	// Pre-allocate the slice with estimated capacity
	allGames := make([]*types.Game, 0, 50)

	var cursor string
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
		response, err := g.roAPI.Games().GetUserGames(context.Background(), builder.Build())
		if err != nil {
			return nil, err
		}

		// Append games from this page
		for _, game := range response.Data {
			allGames = append(allGames, &game)
		}

		// Check if there are more pages
		if response.NextPageCursor == nil || *response.NextPageCursor == "" {
			break
		}

		// Update cursor for next page
		cursor = *response.NextPageCursor
	}

	return allGames, nil
}

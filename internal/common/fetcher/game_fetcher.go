package fetcher

import (
	"context"
	"sync"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/jaxron/roapi.go/pkg/api/resources/games"
	"github.com/jaxron/roapi.go/pkg/api/types"
	"go.uber.org/zap"
)

// GameFetchResult contains the result of fetching a user's games.
type GameFetchResult struct {
	Games []types.Game
	Error error
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

// FetchUserGames retrieves all games for a batch of user IDs.
func (g *GameFetcher) FetchUserGames(userIDs []uint64) map[uint64][]types.Game {
	var wg sync.WaitGroup
	resultsChan := make(chan struct {
		UserID uint64
		Result *GameFetchResult
	}, len(userIDs))

	// Process each user concurrently
	for _, userID := range userIDs {
		wg.Add(1)
		go func(id uint64) {
			defer wg.Done()

			games, err := g.fetchAllGamesForUser(id)
			resultsChan <- struct {
				UserID uint64
				Result *GameFetchResult
			}{
				UserID: id,
				Result: &GameFetchResult{
					Games: games,
					Error: err,
				},
			}
		}(userID)
	}

	// Close channel when all goroutines complete
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// Collect results from the channel
	results := make(map[uint64][]types.Game)
	successfulFetches := 0
	totalGames := 0

	for result := range resultsChan {
		if result.Result.Error != nil {
			g.logger.Warn("Error fetching user games",
				zap.Uint64("userID", result.UserID),
				zap.Error(result.Result.Error))
			continue
		}

		if len(result.Result.Games) > 0 {
			results[result.UserID] = result.Result.Games
			successfulFetches++
			totalGames += len(result.Result.Games)
		}
	}

	g.logger.Info("Finished fetching user games",
		zap.Int("totalUsers", len(userIDs)),
		zap.Int("successfulFetches", successfulFetches),
		zap.Int("totalGames", totalGames))

	return results
}

// fetchAllGamesForUser retrieves all games for a single user.
func (g *GameFetcher) fetchAllGamesForUser(userID uint64) ([]types.Game, error) {
	var allGames []types.Game
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
		allGames = append(allGames, response.Data...)

		// Check if there are more pages
		if response.NextPageCursor == nil || *response.NextPageCursor == "" {
			break
		}

		// Update cursor for next page
		cursor = *response.NextPageCursor
	}

	return allGames, nil
}

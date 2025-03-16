package sync

import (
	"context"
	"fmt"
	"time"

	"github.com/jaxron/roapi.go/pkg/api/resources/games"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/sourcegraph/conc/pool"
	"go.uber.org/zap"
)

// scanGames periodically scans games for active players.
func (w *Worker) scanGames() {
	for {
		if err := w.processPendingGames(); err != nil {
			w.logger.Error("Failed to process pending games", zap.Error(err))
			time.Sleep(5 * time.Second)
			continue
		}
	}
}

// processPendingGames handles scanning of pending games.
func (w *Worker) processPendingGames() error {
	ctx := context.Background()

	// Get oldest non-deleted games
	pendingGames, err := w.db.Models().Condo().GetAndUpdatePendingGames(ctx, 50)
	if err != nil {
		return fmt.Errorf("failed to get pending games: %w", err)
	}

	if len(pendingGames) == 0 {
		// No games to process, wait before checking again
		time.Sleep(1 * time.Second)
		return nil
	}

	// Get place details
	placeIDs := make([]uint64, len(pendingGames))
	for i, game := range pendingGames {
		placeIDs[i] = game.ID
	}

	details, err := w.roAPI.Games().GetMultiplePlaceDetails(ctx, placeIDs)
	if err != nil {
		return fmt.Errorf("failed to get place details: %w", err)
	}

	// Process each place
	p := pool.New().WithContext(ctx)
	for _, detail := range details {
		p.Go(func(ctx context.Context) error {
			// Check if game is deleted
			if !detail.IsPlayable && detail.ReasonProhibited == "AssetUnapproved" {
				if err := w.db.Models().Condo().MarkGameDeleted(ctx, detail.PlaceID); err != nil {
					return fmt.Errorf("failed to mark game %d as deleted: %w", detail.PlaceID, err)
				}

				w.logger.Debug("Marked game as deleted",
					zap.Uint64("game_id", detail.PlaceID),
					zap.String("reason", detail.ReasonProhibited))
				return nil
			}

			// Build request parameters
			serverParams := games.NewGameServersBuilder(detail.PlaceID).
				WithServerType(games.ServerTypePublic).
				WithSortOrder(games.SortOrderDesc).
				WithExcludeFullGames(false).
				WithLimit(100). // These games don't have much servers so pagination is not needed
				Build()

			// Get active servers
			servers, err := w.roAPI.Games().GetGameServers(ctx, serverParams)
			if err != nil {
				return fmt.Errorf("failed to get game servers for game %d: %w", detail.PlaceID, err)
			}

			// Collect all player tokens
			var tokens []string
			for _, server := range servers.Data {
				tokens = append(tokens, server.PlayerTokens...)
			}

			if len(tokens) == 0 {
				return nil
			}

			w.logger.Debug("Got game players",
				zap.Uint64("game_id", detail.PlaceID),
				zap.Int("server_count", len(servers.Data)),
				zap.Int("token_count", len(tokens)))

			// Get thumbnails for players
			urls := w.thumbnailFetcher.ProcessPlayerTokens(ctx, tokens)
			if len(urls) == 0 {
				return nil
			}

			// Store player thumbnails
			players := make([]*types.CondoPlayer, 0, len(urls))
			uniqueURLs := make(map[string]struct{}, len(urls))
			for _, url := range urls {
				if _, ok := uniqueURLs[url]; ok {
					continue
				}

				players = append(players, &types.CondoPlayer{
					ThumbnailURL: url,
					GameIDs:      []uint64{detail.PlaceID},
					LastUpdated:  time.Now(),
				})
				uniqueURLs[url] = struct{}{}
			}

			if err := w.db.Models().Condo().SaveCondoPlayers(ctx, players); err != nil {
				return fmt.Errorf("failed to save condo players for game %d: %w", detail.PlaceID, err)
			}

			w.logger.Info("Saved condo players for game",
				zap.Uint64("game_id", detail.PlaceID),
				zap.Int("players", len(players)))

			return nil
		})
	}

	// Wait for goroutines
	if err := p.Wait(); err != nil {
		return fmt.Errorf("error processing games: %w", err)
	}

	w.logger.Info("Processed pending games", zap.Int("count", len(pendingGames)))
	return nil
}

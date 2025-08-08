package sync

import (
	"context"
	"fmt"
	"time"

	"github.com/jaxron/roapi.go/pkg/api/resources/games"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/pkg/utils"
	"github.com/sourcegraph/conc/pool"
	"go.uber.org/zap"
)

// runMutualScanner continuously runs full scans for users.
func (w *Worker) runMutualScanner(ctx context.Context) {
	for {
		// Check if context was cancelled
		if utils.ContextGuardWithLog(ctx, w.logger, "Context cancelled, stopping mutual scanner") {
			return
		}

		before := time.Now().Add(-1 * time.Hour) // Scan users not checked in the last hour

		userIDs, err := w.db.Model().Sync().GetUsersForFullScan(ctx, before, 100)
		if err != nil {
			w.logger.Error("Failed to get users for full scan", zap.Error(err))

			if utils.ContextSleep(ctx, 5*time.Second) == utils.SleepCancelled {
				w.logger.Info("Context cancelled during error wait, stopping mutual scanner")
				return
			}

			continue
		}

		for _, userID := range userIDs {
			// Check if context was cancelled
			if utils.ContextGuardWithLog(ctx, w.logger, "Context cancelled during user scan, stopping mutual scanner") {
				return
			}

			if !w.scanner.ShouldScan(ctx, userID) {
				continue
			}

			_, err := w.scanner.PerformFullScan(ctx, userID)
			if err != nil {
				w.logger.Error("Failed to perform full scan",
					zap.Error(err),
					zap.Uint64("userID", userID))
			}

			// Sleep to respect rate limits
			if utils.ContextSleep(ctx, 1*time.Second) == utils.SleepCancelled {
				w.logger.Info("Context cancelled during rate limit wait, stopping mutual scanner")
				return
			}
		}

		// Sleep before next batch
		if utils.ContextSleep(ctx, 5*time.Second) == utils.SleepCancelled {
			w.logger.Info("Context cancelled during batch wait, stopping mutual scanner")
			return
		}
	}
}

// runGameScanner periodically scans games for active players.
func (w *Worker) runGameScanner(ctx context.Context) {
	for {
		// Check if context was cancelled
		if utils.ContextGuardWithLog(ctx, w.logger, "Context cancelled, stopping game scanner") {
			return
		}

		if err := w.processPendingGames(ctx); err != nil {
			w.logger.Error("Failed to process pending games", zap.Error(err))

			if utils.ContextSleep(ctx, 5*time.Second) == utils.SleepCancelled {
				w.logger.Info("Context cancelled during error wait, stopping game scanner")
				return
			}

			continue
		}
	}
}

// processPendingGames handles scanning of pending games.
func (w *Worker) processPendingGames(ctx context.Context) error {
	// Get oldest non-deleted games
	pendingGames, err := w.db.Model().Condo().GetAndUpdatePendingGames(ctx, 50)
	if err != nil {
		return fmt.Errorf("failed to get pending games: %w", err)
	}

	if len(pendingGames) == 0 {
		// No games to process, wait before checking again
		if utils.ContextSleep(ctx, 1*time.Second) == utils.SleepCancelled {
			return ctx.Err()
		}

		return nil
	}

	// Get place details
	placeIDs := make([]int64, len(pendingGames))
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
				if err := w.db.Model().Condo().MarkGameDeleted(ctx, detail.PlaceID); err != nil {
					return fmt.Errorf("failed to mark game %d as deleted: %w", detail.PlaceID, err)
				}

				w.logger.Debug("Marked game as deleted",
					zap.Int64("gameID", detail.PlaceID),
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
				zap.Int64("gameID", detail.PlaceID),
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
					GameIDs:      []int64{detail.PlaceID},
					LastUpdated:  time.Now(),
				})
				uniqueURLs[url] = struct{}{}
			}

			if err := w.db.Model().Condo().SaveCondoPlayers(ctx, players); err != nil {
				return fmt.Errorf("failed to save condo players for game %d: %w", detail.PlaceID, err)
			}

			w.logger.Info("Saved condo players for game",
				zap.Int64("gameID", detail.PlaceID),
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

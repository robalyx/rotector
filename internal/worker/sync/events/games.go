package events

import (
	"context"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/pkg/utils"
	"go.uber.org/zap"
)

// handleGameURL processes a potential Roblox game URL from a message.
func (h *Handler) handleGameURL(serverID uint64, content string) {
	// Check if the content contains a game URL
	if !utils.IsRobloxGameURL(content) {
		return
	}

	// Extract the game ID from the URL
	gameID, err := utils.ExtractGameIDFromURL(content)
	if err != nil {
		h.logger.Debug("Failed to extract game ID from URL",
			zap.String("content", content),
			zap.Error(err))

		return
	}

	// Convert game ID to uint64
	placeID, err := strconv.ParseUint(gameID, 10, 64)
	if err != nil {
		h.logger.Error("Failed to parse game ID",
			zap.String("game_id", gameID),
			zap.Error(err))

		return
	}

	// Process the game
	h.processGame(serverID, placeID)
}

// processGame fetches and stores game information.
func (h *Handler) processGame(serverID uint64, placeID uint64) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Get universe ID from place ID
	universeResp, err := h.roAPI.Games().GetUniverseIDFromPlace(ctx, placeID)
	if err != nil {
		h.logger.Error("Failed to get universe ID",
			zap.Uint64("place_id", placeID),
			zap.Error(err))

		return
	}

	// Get game details using universe ID
	gamesResp, err := h.roAPI.Games().GetGamesByUniverseIDs(ctx, []uint64{universeResp.UniverseID})
	if err != nil {
		h.logger.Error("Failed to get game details",
			zap.Uint64("universe_id", universeResp.UniverseID),
			zap.Error(err))

		return
	}

	// Process each game in the response
	now := time.Now()

	for _, gameDetail := range gamesResp.Data {
		// Skip games with more than 50k visits or more than 40 players
		if gameDetail.Visits > 50000 || gameDetail.Playing >= 40 {
			h.logger.Debug("Skipping high-traffic game",
				zap.Uint64("server_id", serverID),
				zap.Uint64("game_id", gameDetail.RootPlaceID),
				zap.String("name", gameDetail.Name),
				zap.Uint64("visits", gameDetail.Visits))

			continue
		}

		game := &types.CondoGame{
			ID:             gameDetail.RootPlaceID,
			UUID:           uuid.New(),
			UniverseID:     gameDetail.ID,
			Name:           gameDetail.Name,
			Description:    gameDetail.Description,
			CreatorID:      gameDetail.Creator.ID,
			CreatorName:    gameDetail.Creator.Name,
			MentionCount:   1,
			LastScanned:    now,
			LastUpdated:    now,
			Visits:         gameDetail.Visits,
			Created:        gameDetail.Created,
			Updated:        gameDetail.Updated,
			FavoritedCount: gameDetail.FavoritedCount,
		}

		// Store the game in the database
		if err := h.db.Model().Condo().SaveGame(ctx, game); err != nil {
			h.logger.Error("Failed to save game",
				zap.Uint64("game_id", game.ID),
				zap.Error(err))

			continue
		}

		h.logger.Debug("Successfully processed and stored game",
			zap.Uint64("server_id", serverID),
			zap.Uint64("game_id", game.ID),
			zap.Uint64("universe_id", game.UniverseID),
			zap.String("name", game.Name),
			zap.Uint64("visits", game.Visits))
	}
}

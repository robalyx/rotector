package models

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/uptrace/bun"
	"go.uber.org/zap"
)

// CondoModel handles database operations for game and player records.
type CondoModel struct {
	db     *bun.DB
	logger *zap.Logger
}

// NewCondo creates a CondoModel.
func NewCondo(db *bun.DB, logger *zap.Logger) *CondoModel {
	return &CondoModel{
		db:     db,
		logger: logger.Named("db_condo"),
	}
}

// SaveGame updates or inserts a game into the database.
func (r *CondoModel) SaveGame(ctx context.Context, game *types.CondoGame) error {
	_, err := r.db.NewInsert().
		Model(game).
		On("CONFLICT (id) DO UPDATE").
		Set("uuid = EXCLUDED.uuid").
		Set("universe_id = EXCLUDED.universe_id").
		Set("name = EXCLUDED.name").
		Set("description = EXCLUDED.description").
		Set("creator_id = EXCLUDED.creator_id").
		Set("creator_name = EXCLUDED.creator_name").
		// Set("last_scanned = EXCLUDED.last_scanned"). // NOTE: We don't want to update this field
		Set("last_updated = EXCLUDED.last_updated").
		Set("is_deleted = EXCLUDED.is_deleted").
		Set("mention_count = COALESCE(?TableAlias.mention_count, 0) + 1").
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to save game: %w", err)
	}

	return nil
}

// GetGameByID retrieves a game by either their numeric ID or UUID.
func (r *CondoModel) GetGameByID(ctx context.Context, gameID string, fields types.GameField) (*types.CondoGame, error) {
	var game types.CondoGame

	query := r.db.NewSelect().
		Model(&game).
		Column(fields.Columns()...)

	// Parse ID or UUID
	if id, err := strconv.ParseUint(gameID, 10, 64); err == nil {
		query.Where("id = ?", id)
	} else {
		// Parse UUID string
		uid, err := uuid.Parse(gameID)
		if err != nil {
			return nil, types.ErrInvalidGameID
		}
		query.Where("uuid = ?", uid)
	}

	err := query.Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, types.ErrGameNotFound
		}
		return nil, fmt.Errorf("failed to get game by ID: %w", err)
	}

	return &game, nil
}

// GetGamesByIDs retrieves specified game information for a list of game IDs.
func (r *CondoModel) GetGamesByIDs(
	ctx context.Context, gameIDs []uint64, fields types.GameField,
) (map[uint64]*types.CondoGame, error) {
	var games []types.CondoGame

	err := r.db.NewSelect().
		Model(&games).
		Column(fields.Columns()...).
		Where("id IN (?)", bun.In(gameIDs)).
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get games by IDs: %w", err)
	}

	// Convert slice to map
	result := make(map[uint64]*types.CondoGame, len(games))
	for i := range games {
		result[games[i].ID] = &games[i]
	}

	return result, nil
}

// GetAndUpdatePendingGames retrieves the oldest non-deleted games and updates their last_scanned time.
func (r *CondoModel) GetAndUpdatePendingGames(ctx context.Context, limit int) ([]*types.CondoGame, error) {
	var games []*types.CondoGame
	now := time.Now()

	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Get the games with row locks
		err := tx.NewSelect().
			Model(&games).
			Where("is_deleted = ?", false).
			Order("last_scanned ASC").
			Limit(limit).
			For("UPDATE SKIP LOCKED").
			Scan(ctx)
		if err != nil {
			return fmt.Errorf("failed to select pending games: %w", err)
		}

		if len(games) == 0 {
			return nil
		}

		// Get the IDs of selected games
		gameIDs := make([]uint64, len(games))
		for i, game := range games {
			gameIDs[i] = game.ID
		}

		// Update last_scanned for selected games
		_, err = tx.NewUpdate().
			Model((*types.CondoGame)(nil)).
			Set("last_scanned = ?", now).
			Where("id IN (?)", bun.In(gameIDs)).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to update last_scanned: %w", err)
		}

		return nil
	})

	return games, err
}

// MarkGameDeleted marks a game as deleted.
func (r *CondoModel) MarkGameDeleted(ctx context.Context, gameID uint64) error {
	_, err := r.db.NewUpdate().
		Model((*types.CondoGame)(nil)).
		Set("is_deleted = ?", true).
		Where("id = ?", gameID).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to mark game as deleted: %w", err)
	}
	return nil
}

// SaveCondoPlayers saves or updates condo player records, skipping blacklisted players.
func (r *CondoModel) SaveCondoPlayers(ctx context.Context, players []*types.CondoPlayer) error {
	_, err := r.db.NewInsert().
		Model(&players).
		On("CONFLICT (thumbnail_url) DO UPDATE").
		Set("game_ids = ARRAY(SELECT DISTINCT unnest(EXCLUDED.game_ids || ?TableAlias.game_ids))").
		Set("last_updated = EXCLUDED.last_updated").
		Where("?TableAlias.is_blacklisted = ?", false).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to save condo players: %w", err)
	}
	return nil
}

// GetPlayerByThumbnail retrieves a condo player by their thumbnail URL.
func (r *CondoModel) GetPlayerByThumbnail(ctx context.Context, thumbnailURL string) (*types.CondoPlayer, error) {
	var player types.CondoPlayer
	err := r.db.NewSelect().
		Model(&player).
		Where("thumbnail_url = ?", thumbnailURL).
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get player by thumbnail: %w", err)
	}
	return &player, nil
}

// BlacklistPlayer marks a condo player as blacklisted.
func (r *CondoModel) BlacklistPlayer(ctx context.Context, thumbnailURL string) error {
	_, err := r.db.NewUpdate().
		Model((*types.CondoPlayer)(nil)).
		Set("is_blacklisted = ?", true).
		Where("thumbnail_url = ?", thumbnailURL).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to blacklist player: %w", err)
	}
	return nil
}

// SetPlayerUserID updates a condo player's user ID.
func (r *CondoModel) SetPlayerUserID(ctx context.Context, thumbnailURL string, userID uint64) error {
	_, err := r.db.NewUpdate().
		Model((*types.CondoPlayer)(nil)).
		Set("user_id = ?", userID).
		Where("thumbnail_url = ?", thumbnailURL).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to set player user ID: %w", err)
	}
	return nil
}

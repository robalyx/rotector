package models

import (
	"context"
	"database/sql"
	"errors"

	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/uptrace/bun"
	"go.uber.org/zap"
)

// BanModel handles database operations for Discord user bans.
type BanModel struct {
	db     *bun.DB
	logger *zap.Logger
}

// NewBan creates a new BanModel instance.
func NewBan(db *bun.DB, logger *zap.Logger) *BanModel {
	return &BanModel{
		db:     db,
		logger: logger,
	}
}

// BanUser creates or updates a ban record for a Discord user.
func (m *BanModel) BanUser(ctx context.Context, record *types.DiscordBan) error {
	_, err := m.db.NewInsert().
		Model(record).
		On("CONFLICT (id) DO UPDATE").
		Set("reason = EXCLUDED.reason").
		Set("source = EXCLUDED.source").
		Set("notes = EXCLUDED.notes").
		Set("banned_by = EXCLUDED.banned_by").
		Set("banned_at = EXCLUDED.banned_at").
		Set("expires_at = EXCLUDED.expires_at").
		Set("updated_at = EXCLUDED.updated_at").
		Exec(ctx)
	if err != nil {
		return err
	}
	return nil
}

// UnbanUser removes a ban record for a Discord user.
// Returns true if a ban was removed, false if the user wasn't banned.
func (m *BanModel) UnbanUser(ctx context.Context, userID uint64) (bool, error) {
	result, err := m.db.NewDelete().
		Model((*types.DiscordBan)(nil)).
		Where("id = ?", userID).
		Exec(ctx)
	if err != nil {
		return false, err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}

	return affected > 0, nil
}

// IsBanned checks if a Discord user is currently banned.
func (m *BanModel) IsBanned(ctx context.Context, userID uint64) (bool, error) {
	exists, err := m.db.NewSelect().
		Model((*types.DiscordBan)(nil)).
		Where("id = ?", userID).
		Exists(ctx)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}

	return exists, nil
}

// GetBan retrieves the ban record for a Discord user.
func (m *BanModel) GetBan(ctx context.Context, userID uint64) (*types.DiscordBan, error) {
	var ban types.DiscordBan
	err := m.db.NewSelect().
		Model(&ban).
		Where("id = ?", userID).
		Scan(ctx)
	if err != nil {
		return nil, err
	}

	return &ban, nil
}

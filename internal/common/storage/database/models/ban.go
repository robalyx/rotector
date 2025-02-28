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
		Set("expires_at = EXCLUDED.expires_at").
		Set("updated_at = EXCLUDED.updated_at").
		Exec(ctx)
	if err != nil {
		return err
	}
	return nil
}

// BulkBanUsers creates or updates multiple ban records in a single operation.
func (m *BanModel) BulkBanUsers(ctx context.Context, records []*types.DiscordBan) error {
	if len(records) == 0 {
		return nil
	}

	_, err := m.db.NewInsert().
		Model(&records).
		On("CONFLICT (id) DO UPDATE").
		Set("reason = EXCLUDED.reason").
		Set("source = EXCLUDED.source").
		Set("notes = EXCLUDED.notes").
		Set("banned_by = EXCLUDED.banned_by").
		Set("expires_at = EXCLUDED.expires_at").
		Set("updated_at = EXCLUDED.updated_at").
		Exec(ctx)

	return err
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

// BulkCheckBanned efficiently checks whether multiple users are banned.
// Returns a map of user IDs to their banned status.
func (m *BanModel) BulkCheckBanned(ctx context.Context, userIDs []uint64) (map[uint64]bool, error) {
	if len(userIDs) == 0 {
		return map[uint64]bool{}, nil
	}

	// Initialize result map with all users set to not banned
	result := make(map[uint64]bool, len(userIDs))
	for _, id := range userIDs {
		result[id] = false
	}

	// Query to find all banned users from the input list
	var bannedUsers []struct {
		ID uint64 `bun:"id"`
	}

	err := m.db.NewSelect().
		Model((*types.DiscordBan)(nil)).
		Column("id").
		Where("id IN (?)", bun.In(userIDs)).
		Scan(ctx, &bannedUsers)
	if err != nil {
		return nil, err
	}

	// Mark users as banned in the result map
	for _, user := range bannedUsers {
		result[user.ID] = true
	}

	return result, nil
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

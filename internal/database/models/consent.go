package models

import (
	"context"
	"fmt"

	"github.com/robalyx/rotector/internal/database/dbretry"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/uptrace/bun"
	"go.uber.org/zap"
)

// ConsentModel handles database operations for user consent records.
type ConsentModel struct {
	db     *bun.DB
	logger *zap.Logger
}

// NewConsent creates a new consent model.
func NewConsent(db *bun.DB, logger *zap.Logger) *ConsentModel {
	return &ConsentModel{
		db:     db,
		logger: logger.Named("db_consent"),
	}
}

// HasConsented checks if a user has consented to the terms.
func (m *ConsentModel) HasConsented(ctx context.Context, discordUserID uint64) (bool, error) {
	return dbretry.Operation(ctx, func(ctx context.Context) (bool, error) {
		exists, err := m.db.NewSelect().
			Model((*types.UserConsent)(nil)).
			Where("discord_user_id = ?", discordUserID).
			Exists(ctx)
		if err != nil {
			return false, fmt.Errorf("failed to check consent: %w", err)
		}

		return exists, nil
	})
}

// SaveConsent saves a user's consent record.
func (m *ConsentModel) SaveConsent(ctx context.Context, consent *types.UserConsent) error {
	return dbretry.NoResult(ctx, func(ctx context.Context) error {
		_, err := m.db.NewInsert().
			Model(consent).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to save consent: %w", err)
		}

		return nil
	})
}

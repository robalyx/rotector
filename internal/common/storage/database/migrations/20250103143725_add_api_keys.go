package migrations

import (
	"context"
	"fmt"

	"github.com/uptrace/bun"
)

func init() {
	Migrations.MustRegister(func(ctx context.Context, db *bun.DB) error {
		// Add api_keys column to bot_settings table
		_, err := db.NewRaw(`
			ALTER TABLE bot_settings
			ADD COLUMN IF NOT EXISTS api_keys JSONB NOT NULL DEFAULT '[]'::jsonb;
		`).Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to add api_keys column: %w", err)
		}

		return nil
	}, func(ctx context.Context, db *bun.DB) error {
		// Remove api_keys column from bot_settings table
		_, err := db.NewRaw(`
			ALTER TABLE bot_settings
			DROP COLUMN IF EXISTS api_keys;
		`).Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to remove api_keys column: %w", err)
		}

		return nil
	})
}

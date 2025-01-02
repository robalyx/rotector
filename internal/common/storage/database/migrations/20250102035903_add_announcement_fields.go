package migrations

import (
	"context"
	"fmt"

	"github.com/uptrace/bun"
)

func init() {
	Migrations.MustRegister(func(ctx context.Context, db *bun.DB) error {
		// Add announcement type and message columns with default values
		_, err := db.NewRaw(`
			ALTER TABLE bot_settings
			ADD COLUMN announcement_type text NOT NULL DEFAULT 'none',
			ADD COLUMN announcement_message text NOT NULL DEFAULT '';
		`).Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to add announcement columns: %w", err)
		}

		return nil
	}, func(ctx context.Context, db *bun.DB) error {
		// Drop the announcement columns
		_, err := db.NewRaw(`
			ALTER TABLE bot_settings
			DROP COLUMN announcement_type,
			DROP COLUMN announcement_message;
		`).Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to drop announcement columns: %w", err)
		}

		return nil
	})
}

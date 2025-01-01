package migrations

import (
	"context"
	"fmt"

	"github.com/uptrace/bun"
)

func init() {
	Migrations.MustRegister(func(ctx context.Context, db *bun.DB) error {
		// Create index for optimizing appeal rejection cooldown queries
		_, err := db.NewRaw(`
			CREATE INDEX IF NOT EXISTS idx_appeals_rejected_reviewed_at 
			ON appeals (reviewed_at DESC);
		`).Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to create appeal reviewed_at index: %w", err)
		}

		return nil
	}, func(ctx context.Context, db *bun.DB) error {
		// Drop the index when rolling back
		_, err := db.NewRaw(`
			DROP INDEX IF EXISTS idx_appeals_rejected_reviewed_at;
		`).Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to drop appeal reviewed_at index: %w", err)
		}

		return nil
	})
}

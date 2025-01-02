package migrations

import (
	"context"
	"fmt"

	"github.com/uptrace/bun"
)

func init() {
	Migrations.MustRegister(func(ctx context.Context, db *bun.DB) error {
		// Add UUID columns to all user and group tables
		tables := []string{
			"flagged_users",
			"confirmed_users",
			"cleared_users",
			"banned_users",
			"flagged_groups",
			"confirmed_groups",
			"cleared_groups",
			"locked_groups",
		}

		for _, table := range tables {
			// Add UUID column
			_, err := db.NewRaw(fmt.Sprintf(`
				ALTER TABLE %s 
				ADD COLUMN IF NOT EXISTS uuid TEXT NOT NULL DEFAULT gen_random_uuid()
			`, table)).Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to add UUID column to %s: %w", table, err)
			}

			// Create unique index on both id and uuid
			_, err = db.NewRaw(fmt.Sprintf(`
				CREATE UNIQUE INDEX IF NOT EXISTS %s_uuid_idx ON %s (id, uuid)
			`, table, table)).Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to create UUID index on %s: %w", table, err)
			}

			// Create a separate non-unique index on uuid for lookups
			_, err = db.NewRaw(fmt.Sprintf(`
				CREATE INDEX IF NOT EXISTS %s_uuid_lookup_idx ON %s (uuid)
			`, table, table)).Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to create UUID lookup index on %s: %w", table, err)
			}
		}

		return nil
	}, func(ctx context.Context, db *bun.DB) error {
		// Remove UUID columns from all user and group tables
		tables := []string{
			"flagged_users",
			"confirmed_users",
			"cleared_users",
			"banned_users",
			"flagged_groups",
			"confirmed_groups",
			"cleared_groups",
			"locked_groups",
		}

		for _, table := range tables {
			// Drop indexes
			_, err := db.NewRaw(fmt.Sprintf(`
				DROP INDEX IF EXISTS %s_uuid_idx;
				DROP INDEX IF EXISTS %s_uuid_lookup_idx
			`, table, table)).Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to drop UUID indexes from %s: %w", table, err)
			}

			// Drop column
			_, err = db.NewRaw(fmt.Sprintf(`
				ALTER TABLE %s DROP COLUMN IF EXISTS uuid
			`, table)).Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to drop UUID column from %s: %w", table, err)
			}
		}

		return nil
	})
}

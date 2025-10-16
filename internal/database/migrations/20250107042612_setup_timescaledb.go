package migrations

import (
	"context"
	"fmt"

	"github.com/uptrace/bun"
)

func init() {
	Migrations.MustRegister(func(ctx context.Context, db *bun.DB) error {
		// Create TimescaleDB extension
		_, err := db.NewRaw(`CREATE EXTENSION IF NOT EXISTS timescaledb`).Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to create TimescaleDB extension: %w", err)
		}

		// Create hypertables
		_, err = db.NewRaw(`
			SELECT create_hypertable('activity_logs', 'activity_timestamp',
				chunk_time_interval => INTERVAL '1 day',
				if_not_exists => TRUE
			);
		`).Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to create hypertables: %w", err)
		}

		return nil
	}, func(ctx context.Context, db *bun.DB) error {
		// Convert hypertables back to regular tables
		_, err := db.NewRaw(`
			-- Convert hypertables back to regular tables
			CREATE TABLE activity_logs_backup AS SELECT * FROM activity_logs;
			DROP TABLE activity_logs CASCADE;
			ALTER TABLE activity_logs_backup RENAME TO activity_logs;
			-- Drop the extension
			DROP EXTENSION IF EXISTS timescaledb;
		`).Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to revert TimescaleDB setup: %w", err)
		}
		return nil
	})
}

package migrations

import (
	"context"
	"fmt"

	"github.com/rotector/rotector/internal/common/storage/database/types"
	"github.com/uptrace/bun"
)

func init() {
	Migrations.MustRegister(func(ctx context.Context, db *bun.DB) error {
		// Check if the old table exists
		var exists bool
		err := db.NewRaw(`
			SELECT EXISTS (
				SELECT FROM information_schema.tables 
				WHERE table_name = 'user_activity_logs'
			)
		`).Scan(ctx, &exists)
		if err != nil {
			return fmt.Errorf("failed to check if table exists: %w", err)
		}

		// Rename the table if it exists
		if exists {
			_, err = db.NewRaw(`ALTER TABLE user_activity_logs RENAME TO activity_logs`).Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to rename activity logs table: %w", err)
			}
			return nil
		}

		// Create new composite indexes for GetRecentlyReviewedIDs
		_, err = db.NewRaw(`
			CREATE INDEX IF NOT EXISTS idx_activity_logs_user_viewed 
			ON activity_logs (reviewer_id, activity_timestamp DESC, activity_type, user_id)
			WHERE user_id > 0 AND activity_type = ?;

			CREATE INDEX IF NOT EXISTS idx_activity_logs_group_viewed 
			ON activity_logs (reviewer_id, activity_timestamp DESC, activity_type, group_id)
			WHERE group_id > 0 AND activity_type = ?;
		`, types.ActivityTypeUserViewed, types.ActivityTypeGroupViewed).Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to create activity logs indexes: %w", err)
		}

		return nil
	}, func(ctx context.Context, db *bun.DB) error {
		// Drop the new indexes first
		_, err := db.NewRaw(`
			DROP INDEX IF EXISTS idx_activity_logs_user_viewed;
			DROP INDEX IF EXISTS idx_activity_logs_group_viewed;
		`).Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to drop activity logs indexes: %w", err)
		}

		// Check if the new table exists
		var exists bool
		err = db.NewRaw(`
			SELECT EXISTS (
				SELECT FROM information_schema.tables 
				WHERE table_name = 'user_activity_logs'
			)
		`).Scan(ctx, &exists)
		if err != nil {
			return fmt.Errorf("failed to check if table exists: %w", err)
		}

		// Rename the table back if it exists
		if exists {
			_, err = db.NewRaw(`ALTER TABLE activity_logs RENAME TO user_activity_logs`).Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to rename activity logs table back: %w", err)
			}
			return nil
		}

		return nil
	})
}

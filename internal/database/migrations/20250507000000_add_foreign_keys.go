package migrations

import (
	"context"
	"fmt"

	"github.com/uptrace/bun"
)

func init() {
	Migrations.MustRegister(func(ctx context.Context, db *bun.DB) error {
		// Clean up orphaned records to avoid FK constraint violations

		// Clean up user_reasons that reference non-existent users
		for i := range 8 {
			_, err := db.NewRaw(fmt.Sprintf(`
				DELETE FROM user_reasons_%d ur
				WHERE NOT EXISTS (
					SELECT 1 FROM users_%d u
					WHERE u.id = ur.user_id
				)
			`, i, i)).Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to clean up orphaned user_reasons_%d: %w", i, err)
			}
		}

		// Clean up user_verifications that reference non-existent users
		for i := range 8 {
			_, err := db.NewRaw(fmt.Sprintf(`
				DELETE FROM user_verifications_%d uv
				WHERE NOT EXISTS (
					SELECT 1 FROM users_%d u
					WHERE u.id = uv.user_id
				)
			`, i, i)).Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to clean up orphaned user_verifications_%d: %w", i, err)
			}
		}

		// Clean up user_clearances that reference non-existent users
		for i := range 8 {
			_, err := db.NewRaw(fmt.Sprintf(`
				DELETE FROM user_clearances_%d uc
				WHERE NOT EXISTS (
					SELECT 1 FROM users_%d u
					WHERE u.id = uc.user_id
				)
			`, i, i)).Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to clean up orphaned user_clearances_%d: %w", i, err)
			}
		}

		// Clean up user_groups that reference non-existent users
		for i := range 8 {
			_, err := db.NewRaw(fmt.Sprintf(`
				DELETE FROM user_groups_%d ug
				WHERE NOT EXISTS (
					SELECT 1 FROM users_%d u
					WHERE u.id = ug.user_id
				)
			`, i, i)).Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to clean up orphaned user_groups_%d: %w", i, err)
			}
		}

		// Clean up user_outfits that reference non-existent users
		for i := range 8 {
			_, err := db.NewRaw(fmt.Sprintf(`
				DELETE FROM user_outfits_%d uo
				WHERE NOT EXISTS (
					SELECT 1 FROM users_%d u
					WHERE u.id = uo.user_id
				)
			`, i, i)).Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to clean up orphaned user_outfits_%d: %w", i, err)
			}
		}

		// Clean up user_assets that reference non-existent users
		for i := range 8 {
			_, err := db.NewRaw(fmt.Sprintf(`
				DELETE FROM user_assets_%d ua
				WHERE NOT EXISTS (
					SELECT 1 FROM users_%d u
					WHERE u.id = ua.user_id
				)
			`, i, i)).Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to clean up orphaned user_assets_%d: %w", i, err)
			}
		}

		// Clean up user_friends that reference non-existent users
		for i := range 8 {
			_, err := db.NewRaw(fmt.Sprintf(`
				DELETE FROM user_friends_%d uf
				WHERE NOT EXISTS (
					SELECT 1 FROM users_%d u
					WHERE u.id = uf.user_id
				)
			`, i, i)).Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to clean up orphaned user_friends_%d: %w", i, err)
			}
		}

		// Clean up user_games that reference non-existent users
		for i := range 8 {
			_, err := db.NewRaw(fmt.Sprintf(`
				DELETE FROM user_games_%d ug
				WHERE NOT EXISTS (
					SELECT 1 FROM users_%d u
					WHERE u.id = ug.user_id
				)
			`, i, i)).Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to clean up orphaned user_games_%d: %w", i, err)
			}
		}

		// Clean up user_favorites that reference non-existent users
		for i := range 8 {
			_, err := db.NewRaw(fmt.Sprintf(`
				DELETE FROM user_favorites_%d uf
				WHERE NOT EXISTS (
					SELECT 1 FROM users_%d u
					WHERE u.id = uf.user_id
				)
			`, i, i)).Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to clean up orphaned user_favorites_%d: %w", i, err)
			}
		}

		// Clean up user_inventories that reference non-existent users
		for i := range 8 {
			_, err := db.NewRaw(fmt.Sprintf(`
				DELETE FROM user_inventories_%d ui
				WHERE NOT EXISTS (
					SELECT 1 FROM users_%d u
					WHERE u.id = ui.user_id
				)
			`, i, i)).Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to clean up orphaned user_inventories_%d: %w", i, err)
			}
		}

		// Add foreign key constraints to user relationship tables that reference users table
		constraints := []struct {
			table       string
			column      string
			refTable    string
			refColumn   string
			onDelete    string
			description string
		}{
			// User reasons
			{
				table:       "user_reasons",
				column:      "user_id",
				refTable:    "users",
				refColumn:   "id",
				onDelete:    "CASCADE",
				description: "user_reasons.user_id references users.id",
			},
			// User verifications
			{
				table:       "user_verifications",
				column:      "user_id",
				refTable:    "users",
				refColumn:   "id",
				onDelete:    "CASCADE",
				description: "user_verifications.user_id references users.id",
			},
			// User clearances
			{
				table:       "user_clearances",
				column:      "user_id",
				refTable:    "users",
				refColumn:   "id",
				onDelete:    "CASCADE",
				description: "user_clearances.user_id references users.id",
			},
			// Other user relations
			{
				table:       "user_groups",
				column:      "user_id",
				refTable:    "users",
				refColumn:   "id",
				onDelete:    "CASCADE",
				description: "user_groups.user_id references users.id",
			},
			{
				table:       "user_outfits",
				column:      "user_id",
				refTable:    "users",
				refColumn:   "id",
				onDelete:    "CASCADE",
				description: "user_outfits.user_id references users.id",
			},
			{
				table:       "user_assets",
				column:      "user_id",
				refTable:    "users",
				refColumn:   "id",
				onDelete:    "CASCADE",
				description: "user_assets.user_id references users.id",
			},
			{
				table:       "user_friends",
				column:      "user_id",
				refTable:    "users",
				refColumn:   "id",
				onDelete:    "CASCADE",
				description: "user_friends.user_id references users.id",
			},
			{
				table:       "user_games",
				column:      "user_id",
				refTable:    "users",
				refColumn:   "id",
				onDelete:    "CASCADE",
				description: "user_games.user_id references users.id",
			},
			{
				table:       "user_favorites",
				column:      "user_id",
				refTable:    "users",
				refColumn:   "id",
				onDelete:    "CASCADE",
				description: "user_favorites.user_id references users.id",
			},
			{
				table:       "user_inventories",
				column:      "user_id",
				refTable:    "users",
				refColumn:   "id",
				onDelete:    "CASCADE",
				description: "user_inventories.user_id references users.id",
			},
		}

		// For each partition, add foreign key constraint
		for _, constraint := range constraints {
			for i := range 8 {
				tableName := fmt.Sprintf("%s_%d", constraint.table, i)
				refTablePartition := fmt.Sprintf("%s_%d", constraint.refTable, i)

				constraintName := fmt.Sprintf("%s_%d_%s_to_%s_fkey", constraint.table, i, constraint.column, constraint.refTable)

				// Add constraint as NOT VALID
				_, err := db.NewRaw(fmt.Sprintf(`
					ALTER TABLE %s
					ADD CONSTRAINT %s
					FOREIGN KEY (%s) REFERENCES %s (%s)
					ON DELETE %s
					NOT VALID
				`, tableName, constraintName, constraint.column, refTablePartition, constraint.refColumn, constraint.onDelete)).Exec(ctx)
				if err != nil {
					return fmt.Errorf("failed to add foreign key constraint %s: %w", constraintName, err)
				}

				// Validate the constraint
				_, err = db.NewRaw(fmt.Sprintf(`
					ALTER TABLE %s
					VALIDATE CONSTRAINT %s
				`, tableName, constraintName)).Exec(ctx)
				if err != nil {
					return fmt.Errorf("failed to validate constraint %s on table %s: %w", constraintName, tableName, err)
				}
			}
		}

		return nil
	}, func(ctx context.Context, db *bun.DB) error {
		// Drop all created foreign key constraints
		constraints := []struct {
			table    string
			refTable string
		}{
			{"user_reasons", "users"},
			{"user_verifications", "users"},
			{"user_clearances", "users"},
			{"user_groups", "users"},
			{"user_outfits", "users"},
			{"user_assets", "users"},
			{"user_friends", "users"},
			{"user_games", "users"},
			{"user_favorites", "users"},
			{"user_inventories", "users"},
		}

		for _, constraint := range constraints {
			for i := range 8 {
				tableName := fmt.Sprintf("%s_%d", constraint.table, i)
				constraintName := fmt.Sprintf("%s_%d_user_id_to_%s_fkey", constraint.table, i, constraint.refTable)

				// Drop the foreign key constraint
				_, err := db.NewRaw(fmt.Sprintf(
					"ALTER TABLE %s DROP CONSTRAINT IF EXISTS %s",
					tableName,
					constraintName,
				)).Exec(ctx)
				if err != nil {
					return fmt.Errorf("failed to drop foreign key constraint %s: %w", constraintName, err)
				}
			}
		}

		return nil
	})
}

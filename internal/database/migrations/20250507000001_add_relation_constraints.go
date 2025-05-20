package migrations

import (
	"context"
	"fmt"

	"github.com/uptrace/bun"
)

func init() {
	Migrations.MustRegister(func(ctx context.Context, db *bun.DB) error {
		// Clean up orphaned data in reference tables
		for i := range 8 {
			// Clean up group_infos that have no related user_groups
			_, err := db.NewRaw(fmt.Sprintf(`
				DELETE FROM group_infos_%d AS gi
				WHERE NOT EXISTS (
					SELECT 1 FROM user_groups ug
					WHERE ug.group_id = gi.id
				)
			`, i)).Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to clean up orphaned group_infos_%d: %w", i, err)
			}

			// Clean up outfit_infos that have no related user_outfits
			_, err = db.NewRaw(fmt.Sprintf(`
				DELETE FROM outfit_infos_%d AS oi
				WHERE NOT EXISTS (
					SELECT 1 FROM user_outfits uo
					WHERE uo.outfit_id = oi.id
				)
			`, i)).Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to clean up orphaned outfit_infos_%d: %w", i, err)
			}

			// Clean up asset_infos that have no related user_assets or outfit_assets
			_, err = db.NewRaw(fmt.Sprintf(`
				DELETE FROM asset_infos_%d AS ai
				WHERE NOT EXISTS (
					SELECT 1 FROM user_assets ua
					WHERE ua.asset_id = ai.id
				) AND NOT EXISTS (
					SELECT 1 FROM outfit_assets oa
					WHERE oa.asset_id = ai.id
				)
			`, i)).Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to clean up orphaned asset_infos_%d: %w", i, err)
			}

			// Clean up friend_infos that have no related user_friends
			_, err = db.NewRaw(fmt.Sprintf(`
				DELETE FROM friend_infos_%d AS fi
				WHERE NOT EXISTS (
					SELECT 1 FROM user_friends uf
					WHERE uf.friend_id = fi.id
				)
			`, i)).Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to clean up orphaned friend_infos_%d: %w", i, err)
			}

			// Clean up game_infos that have no related user_games or user_favorites
			_, err = db.NewRaw(fmt.Sprintf(`
				DELETE FROM game_infos_%d AS gi
				WHERE NOT EXISTS (
					SELECT 1 FROM user_games ug
					WHERE ug.game_id = gi.id
				) AND NOT EXISTS (
					SELECT 1 FROM user_favorites uf
					WHERE uf.game_id = gi.id
				)
			`, i)).Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to clean up orphaned game_infos_%d: %w", i, err)
			}

			// Clean up inventory_infos that have no related user_inventories
			_, err = db.NewRaw(fmt.Sprintf(`
				DELETE FROM inventory_infos_%d AS ii
				WHERE NOT EXISTS (
					SELECT 1 FROM user_inventories ui
					WHERE ui.inventory_id = ii.id
				)
			`, i)).Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to clean up orphaned inventory_infos_%d: %w", i, err)
			}

			// Clean up outfit_assets that reference non-existent assets
			_, err = db.NewRaw(fmt.Sprintf(`
				DELETE FROM outfit_assets_%d AS oa
				WHERE NOT EXISTS (
					SELECT 1 FROM asset_infos ai
					WHERE ai.id = oa.asset_id
				)
			`, i)).Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to clean up orphaned outfit_assets_%d: %w", i, err)
			}

			// Clean up outfit_assets that reference non-existent outfits
			_, err = db.NewRaw(fmt.Sprintf(`
				DELETE FROM outfit_assets_%d AS oa
				WHERE NOT EXISTS (
					SELECT 1 FROM outfit_infos oi
					WHERE oi.id = oa.outfit_id
				)
			`, i)).Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to clean up orphaned outfit_assets_%d: %w", i, err)
			}

			// Also clean up the references from user tables to entity tables

			// Clean up user_groups that reference non-existent groups
			_, err = db.NewRaw(fmt.Sprintf(`
				DELETE FROM user_groups_%d AS ug
				WHERE NOT EXISTS (
					SELECT 1 FROM group_infos gi
					WHERE gi.id = ug.group_id
				)
			`, i)).Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to clean up orphaned user_groups_%d: %w", i, err)
			}

			// Clean up user_outfits that reference non-existent outfits
			_, err = db.NewRaw(fmt.Sprintf(`
				DELETE FROM user_outfits_%d AS uo
				WHERE NOT EXISTS (
					SELECT 1 FROM outfit_infos oi
					WHERE oi.id = uo.outfit_id
				)
			`, i)).Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to clean up orphaned user_outfits_%d: %w", i, err)
			}

			// Clean up user_assets that reference non-existent assets
			_, err = db.NewRaw(fmt.Sprintf(`
				DELETE FROM user_assets_%d AS ua
				WHERE NOT EXISTS (
					SELECT 1 FROM asset_infos ai
					WHERE ai.id = ua.asset_id
				)
			`, i)).Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to clean up orphaned user_assets_%d: %w", i, err)
			}

			// Clean up user_friends that reference non-existent friends
			_, err = db.NewRaw(fmt.Sprintf(`
				DELETE FROM user_friends_%d AS uf
				WHERE NOT EXISTS (
					SELECT 1 FROM friend_infos fi
					WHERE fi.id = uf.friend_id
				)
			`, i)).Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to clean up orphaned user_friends_%d: %w", i, err)
			}

			// Clean up user_games that reference non-existent games
			_, err = db.NewRaw(fmt.Sprintf(`
				DELETE FROM user_games_%d AS ug
				WHERE NOT EXISTS (
					SELECT 1 FROM game_infos gi
					WHERE gi.id = ug.game_id
				)
			`, i)).Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to clean up orphaned user_games_%d: %w", i, err)
			}

			// Clean up user_favorites that reference non-existent games
			_, err = db.NewRaw(fmt.Sprintf(`
				DELETE FROM user_favorites_%d AS uf
				WHERE NOT EXISTS (
					SELECT 1 FROM game_infos gi
					WHERE gi.id = uf.game_id
				)
			`, i)).Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to clean up orphaned user_favorites_%d: %w", i, err)
			}

			// Clean up user_inventories that reference non-existent inventory items
			_, err = db.NewRaw(fmt.Sprintf(`
				DELETE FROM user_inventories_%d AS ui
				WHERE NOT EXISTS (
					SELECT 1 FROM inventory_infos ii
					WHERE ii.id = ui.inventory_id
				)
			`, i)).Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to clean up orphaned user_inventories_%d: %w", i, err)
			}
		}

		// Add foreign key constraints to all relationship tables
		constraints := []struct {
			table       string
			column      string
			refTable    string
			refColumn   string
			onDelete    string
			description string
		}{
			// User groups
			{
				table:       "user_groups",
				column:      "group_id",
				refTable:    "group_infos",
				refColumn:   "id",
				onDelete:    "CASCADE",
				description: "user_groups.group_id references group_infos.id",
			},
			// User outfits
			{
				table:       "user_outfits",
				column:      "outfit_id",
				refTable:    "outfit_infos",
				refColumn:   "id",
				onDelete:    "CASCADE",
				description: "user_outfits.outfit_id references outfit_infos.id",
			},
			// User assets
			{
				table:       "user_assets",
				column:      "asset_id",
				refTable:    "asset_infos",
				refColumn:   "id",
				onDelete:    "CASCADE",
				description: "user_assets.asset_id references asset_infos.id",
			},
			// User friends
			{
				table:       "user_friends",
				column:      "friend_id",
				refTable:    "friend_infos",
				refColumn:   "id",
				onDelete:    "CASCADE",
				description: "user_friends.friend_id references friend_infos.id",
			},
			// User games
			{
				table:       "user_games",
				column:      "game_id",
				refTable:    "game_infos",
				refColumn:   "id",
				onDelete:    "CASCADE",
				description: "user_games.game_id references game_infos.id",
			},
			// User inventory
			{
				table:       "user_inventories",
				column:      "inventory_id",
				refTable:    "inventory_infos",
				refColumn:   "id",
				onDelete:    "CASCADE",
				description: "user_inventories.inventory_id references inventory_infos.id",
			},
			// User favorites
			{
				table:       "user_favorites",
				column:      "game_id",
				refTable:    "game_infos",
				refColumn:   "id",
				onDelete:    "CASCADE",
				description: "user_favorites.game_id references game_infos.id",
			},
			// Outfit assets
			{
				table:       "outfit_assets",
				column:      "asset_id",
				refTable:    "asset_infos",
				refColumn:   "id",
				onDelete:    "CASCADE",
				description: "outfit_assets.asset_id references asset_infos.id",
			},
			// Outfit assets (outfits)
			{
				table:       "outfit_assets",
				column:      "outfit_id",
				refTable:    "outfit_infos",
				refColumn:   "id",
				onDelete:    "CASCADE",
				description: "outfit_assets.outfit_id references outfit_infos.id",
			},
		}

		// Add each constraint to all partitions
		for _, c := range constraints {
			for i := range 8 {
				tableName := fmt.Sprintf("%s_%d", c.table, i)
				constraintName := fmt.Sprintf("%s_%d_%s_to_%s_fkey", c.table, i, c.column, c.refTable)

				// Add constraint as NOT VALID
				_, err := db.NewRaw(fmt.Sprintf(`
					ALTER TABLE %s
					ADD CONSTRAINT %s
					FOREIGN KEY (%s) REFERENCES %s(%s)
					ON DELETE %s
					NOT VALID
				`, tableName, constraintName, c.column, c.refTable, c.refColumn, c.onDelete)).
					Exec(ctx)
				if err != nil {
					return fmt.Errorf("failed to add foreign key constraint %s: %w", constraintName, err)
				}

				// Validate the constraint
				_, err = db.NewRaw(fmt.Sprintf(`
					ALTER TABLE %s
					VALIDATE CONSTRAINT %s
				`, tableName, constraintName)).
					Exec(ctx)
				if err != nil {
					return fmt.Errorf("failed to validate constraint %s on table %s: %w", constraintName, tableName, err)
				}
			}
		}

		return nil
	}, func(ctx context.Context, db *bun.DB) error {
		// Down migration - remove foreign key constraints
		constraints := []struct {
			table    string
			column   string
			refTable string
		}{
			{"user_groups", "group_id", "group_infos"},
			{"user_outfits", "outfit_id", "outfit_infos"},
			{"user_assets", "asset_id", "asset_infos"},
			{"user_friends", "friend_id", "friend_infos"},
			{"user_games", "game_id", "game_infos"},
			{"user_inventories", "inventory_id", "inventory_infos"},
			{"user_favorites", "game_id", "game_infos"},
			{"outfit_assets", "asset_id", "asset_infos"},
			{"outfit_assets", "outfit_id", "outfit_infos"},
		}

		// Remove each constraint from all partitions
		for _, c := range constraints {
			for i := range 8 {
				// Drop foreign key
				_, err := db.NewRaw(fmt.Sprintf(`
					ALTER TABLE %s_%d
					DROP CONSTRAINT IF EXISTS %s_%d_%s_to_%s_fkey
				`, c.table, i, c.table, i, c.column, c.refTable)).
					Exec(ctx)
				if err != nil {
					return fmt.Errorf("failed to drop foreign key constraint from %s_%d.%s: %w",
						c.table, i, c.column, err)
				}
			}
		}

		return nil
	})
}

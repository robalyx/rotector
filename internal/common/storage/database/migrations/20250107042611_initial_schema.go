package migrations

import (
	"context"
	"fmt"
	"strings"

	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/uptrace/bun"
)

func init() {
	Migrations.MustRegister(func(ctx context.Context, db *bun.DB) error {
		// Create partitioned tables
		tables := []struct {
			model interface{}
			name  string
		}{
			{(*types.FlaggedUser)(nil), "flagged_users"},
			{(*types.ConfirmedUser)(nil), "confirmed_users"},
			{(*types.ClearedUser)(nil), "cleared_users"},
			{(*types.BannedUser)(nil), "banned_users"},
			{(*types.FlaggedGroup)(nil), "flagged_groups"},
			{(*types.ConfirmedGroup)(nil), "confirmed_groups"},
			{(*types.ClearedGroup)(nil), "cleared_groups"},
			{(*types.LockedGroup)(nil), "locked_groups"},
		}

		for _, table := range tables {
			_, err := db.NewCreateTable().
				Model(table.model).
				ModelTableExpr(table.name).
				IfNotExists().
				PartitionBy(`HASH (id)`).
				Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to create parent table %s: %w", table.name, err)
			}

			for i := range 8 {
				_, err = db.NewRaw(fmt.Sprintf(`
					CREATE TABLE IF NOT EXISTS %s_%d 
					PARTITION OF %s 
					FOR VALUES WITH (modulus 8, remainder %d)
				`, table.name, i, table.name, i)).Exec(ctx)
				if err != nil {
					return fmt.Errorf("failed to create partition %s_%d: %w", table.name, i, err)
				}
			}
		}

		// Create other tables
		models := []interface{}{
			(*types.HourlyStats)(nil),
			(*types.UserSetting)(nil),
			(*types.BotSetting)(nil),
			(*types.ActivityLog)(nil),
			(*types.Appeal)(nil),
			(*types.AppealMessage)(nil),
			(*types.AppealTimeline)(nil),
			(*types.GroupMemberTracking)(nil),
			(*types.UserReputation)(nil),
			(*types.GroupReputation)(nil),
		}

		for _, model := range models {
			_, err := db.NewCreateTable().
				Model(model).
				IfNotExists().
				Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to create table %T: %w", model, err)
			}
		}

		return nil
	}, func(ctx context.Context, db *bun.DB) error {
		// Down migration - drop all tables
		models := []interface{}{
			(*types.GroupReputation)(nil),
			(*types.UserReputation)(nil),
			(*types.GroupMemberTracking)(nil),
			(*types.AppealTimeline)(nil),
			(*types.AppealMessage)(nil),
			(*types.Appeal)(nil),
			(*types.ActivityLog)(nil),
			(*types.BotSetting)(nil),
			(*types.UserSetting)(nil),
			(*types.HourlyStats)(nil),
		}

		for _, model := range models {
			_, err := db.NewDropTable().
				Model(model).
				IfExists().
				Cascade().
				Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to drop table %T: %w", model, err)
			}
		}

		// Drop partitioned tables
		partitionedTables := []string{
			"locked_groups",
			"cleared_groups",
			"confirmed_groups",
			"flagged_groups",
			"banned_users",
			"cleared_users",
			"confirmed_users",
			"flagged_users",
		}

		var dropStmt strings.Builder
		dropStmt.WriteString("DROP TABLE IF EXISTS ")

		for i, table := range partitionedTables {
			if i > 0 {
				dropStmt.WriteString(", ")
			}
			dropStmt.WriteString(table)
			for j := range 8 {
				dropStmt.WriteString(fmt.Sprintf(", %s_%d", table, j))
			}
		}

		dropStmt.WriteString(" CASCADE")

		_, err := db.NewRaw(dropStmt.String()).Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to drop partitioned tables: %w", err)
		}

		return nil
	})
}

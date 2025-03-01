package migrations

import (
	"context"
	"fmt"
	"strings"

	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/uptrace/bun"
)

func init() { //nolint:funlen
	Migrations.MustRegister(func(ctx context.Context, db *bun.DB) error {
		// Create partitioned tables
		tables := []struct {
			model any
			name  string
		}{
			{(*types.FlaggedUser)(nil), "flagged_users"},
			{(*types.ConfirmedUser)(nil), "confirmed_users"},
			{(*types.ClearedUser)(nil), "cleared_users"},
			{(*types.FlaggedGroup)(nil), "flagged_groups"},
			{(*types.ConfirmedGroup)(nil), "confirmed_groups"},
			{(*types.ClearedGroup)(nil), "cleared_groups"},
			{(*types.GroupMemberTracking)(nil), "group_member_trackings"},
			{(*types.UserReputation)(nil), "user_reputations"},
			{(*types.GroupReputation)(nil), "group_reputations"},
			{(*types.UserVote)(nil), "user_votes"},
			{(*types.GroupVote)(nil), "group_votes"},
			{(*types.DiscordServerMember)(nil), "discord_server_members"},
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
		models := []any{
			(*types.HourlyStats)(nil),
			(*types.UserSetting)(nil),
			(*types.BotSetting)(nil),
			(*types.ActivityLog)(nil),
			(*types.Appeal)(nil),
			(*types.AppealMessage)(nil),
			(*types.AppealTimeline)(nil),
			(*types.DiscordBan)(nil),
			(*types.VoteStats)(nil),
			(*types.MaterializedViewRefresh)(nil),
			(*types.UserConsent)(nil),
			(*types.DiscordServerInfo)(nil),
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
		models := []any{
			(*types.DiscordServerInfo)(nil),
			(*types.UserConsent)(nil),
			(*types.MaterializedViewRefresh)(nil),
			(*types.VoteStats)(nil),
			(*types.DiscordBan)(nil),
			(*types.GroupVote)(nil),
			(*types.UserVote)(nil),
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
			"discord_server_members",
			"group_votes",
			"user_votes",
			"group_reputations",
			"user_reputations",
			"group_member_trackings",
			"cleared_groups",
			"confirmed_groups",
			"flagged_groups",
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

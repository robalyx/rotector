package migrations

import (
	"context"
	"fmt"
	"strings"

	"github.com/robalyx/rotector/internal/database/types"
	"github.com/uptrace/bun"
)

func init() { //nolint:funlen
	Migrations.MustRegister(func(ctx context.Context, db *bun.DB) error {
		// Create partitioned tables
		tables := []struct {
			model        any
			name         string
			partitionKey string
		}{
			{(*types.FlaggedUser)(nil), "flagged_users", "id"},
			{(*types.ConfirmedUser)(nil), "confirmed_users", "id"},
			{(*types.ClearedUser)(nil), "cleared_users", "id"},
			{(*types.FlaggedGroup)(nil), "flagged_groups", "id"},
			{(*types.ConfirmedGroup)(nil), "confirmed_groups", "id"},
			{(*types.ClearedGroup)(nil), "cleared_groups", "id"},
			{(*types.CondoGame)(nil), "condo_games", "id"},
			{(*types.GroupMemberTracking)(nil), "group_member_trackings", "id"},
			{(*types.UserReputation)(nil), "user_reputations", "id"},
			{(*types.GroupReputation)(nil), "group_reputations", "id"},
			{(*types.UserVote)(nil), "user_votes", "id"},
			{(*types.GroupVote)(nil), "group_votes", "id"},
			{(*types.Appeal)(nil), "appeals", "id"},
			{(*types.AppealMessage)(nil), "appeal_messages", "id"},
			{(*types.AppealBlacklist)(nil), "appeal_blacklists", "user_id"},
			{(*types.DiscordServerMember)(nil), "discord_server_members", "user_id"},
			{(*types.DiscordUserRedaction)(nil), "discord_user_redactions", "user_id"},
			{(*types.InappropriateMessage)(nil), "inappropriate_messages", "user_id"},
			{(*types.InappropriateUserSummary)(nil), "inappropriate_user_summaries", "user_id"},
			{(*types.DiscordUserFullScan)(nil), "discord_user_full_scans", "user_id"},
			{(*types.DiscordUserWhitelist)(nil), "discord_user_whitelists", "user_id"},
			{(*types.UserComment)(nil), "user_comments", "target_id"},
			{(*types.GroupComment)(nil), "group_comments", "target_id"},
		}

		for _, table := range tables {
			_, err := db.NewCreateTable().
				Model(table.model).
				ModelTableExpr(table.name).
				IfNotExists().
				PartitionBy(fmt.Sprintf(`HASH (%s)`, table.partitionKey)).
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
			(*types.AppealTimeline)(nil),
			(*types.DiscordBan)(nil),
			(*types.VoteStats)(nil),
			(*types.MaterializedViewRefresh)(nil),
			(*types.UserConsent)(nil),
			(*types.DiscordServerInfo)(nil),
			(*types.GuildBanLog)(nil),
			(*types.CondoPlayer)(nil),
			(*types.IvanMessage)(nil),
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
			(*types.IvanMessage)(nil),
			(*types.CondoPlayer)(nil),
			(*types.GuildBanLog)(nil),
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
			"group_comments",
			"user_comments",
			"discord_user_full_scans",
			"inappropriate_user_summaries",
			"inappropriate_messages",
			"discord_user_redactions",
			"discord_server_members",
			"appeal_blacklists",
			"appeal_messages",
			"appeals",
			"group_votes",
			"user_votes",
			"group_reputations",
			"user_reputations",
			"group_member_trackings",
			"condo_games",
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

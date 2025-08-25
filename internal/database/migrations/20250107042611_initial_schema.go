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
			{(*types.User)(nil), "users", "id"},
			{(*types.UserReason)(nil), "user_reasons", "user_id"},
			{(*types.UserVerification)(nil), "user_verifications", "user_id"},
			{(*types.UserClearance)(nil), "user_clearances", "user_id"},
			{(*types.UserGroup)(nil), "user_groups", "user_id"},
			{(*types.GroupInfo)(nil), "group_infos", "id"},
			{(*types.UserOutfit)(nil), "user_outfits", "user_id"},
			{(*types.OutfitInfo)(nil), "outfit_infos", "id"},
			{(*types.OutfitAsset)(nil), "outfit_assets", "outfit_id"},
			{(*types.UserAsset)(nil), "user_assets", "user_id"},
			{(*types.AssetInfo)(nil), "asset_infos", "id"},
			{(*types.UserFriend)(nil), "user_friends", "user_id"},
			{(*types.FriendInfo)(nil), "friend_infos", "id"},
			{(*types.UserGame)(nil), "user_games", "user_id"},
			{(*types.GameInfo)(nil), "game_infos", "id"},
			{(*types.UserInventory)(nil), "user_inventories", "user_id"},
			{(*types.InventoryInfo)(nil), "inventory_infos", "id"},
			{(*types.UserFavorite)(nil), "user_favorites", "user_id"},
			// {(*types.UserBadge)(nil), "user_badges", "user_id"},
			{(*types.Group)(nil), "groups", "id"},
			{(*types.GroupReason)(nil), "group_reasons", "group_id"},
			{(*types.GroupVerification)(nil), "group_verifications", "group_id"},
			{(*types.GroupMixedClassification)(nil), "group_mixed_classifications", "group_id"},
			{(*types.CondoGame)(nil), "condo_games", "id"},
			{(*types.GroupMemberTracking)(nil), "group_member_trackings", "id"},
			{(*types.GroupMemberTrackingUser)(nil), "group_member_tracking_users", "group_id"},
			{(*types.OutfitAssetTracking)(nil), "outfit_asset_trackings", "id"},
			{(*types.OutfitAssetTrackingOutfit)(nil), "outfit_asset_tracking_outfits", "asset_id"},
			{(*types.GameTracking)(nil), "game_trackings", "id"},
			{(*types.GameTrackingUser)(nil), "game_tracking_users", "game_id"},
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
				PartitionBy(fmt.Sprintf("HASH (%s)", table.partitionKey)).
				Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to create parent table %s: %w", table.name, err)
			}

			for i := range 8 {
				_, err = db.NewRaw(fmt.Sprintf(
					"CREATE TABLE IF NOT EXISTS %s_%d PARTITION OF %s FOR VALUES WITH (modulus 8, remainder %d)",
					table.name, i, table.name, i)).
					Exec(ctx)
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
			(*types.DiscordBan)(nil),
			(*types.MaterializedViewRefresh)(nil),
			(*types.UserConsent)(nil),
			(*types.DiscordServerInfo)(nil),
			(*types.GuildBanLog)(nil),
			(*types.CondoPlayer)(nil),
			(*types.IvanMessage)(nil),
			(*types.ReviewerInfo)(nil),
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
			(*types.ReviewerInfo)(nil),
			(*types.IvanMessage)(nil),
			(*types.CondoPlayer)(nil),
			(*types.GuildBanLog)(nil),
			(*types.DiscordServerInfo)(nil),
			(*types.UserConsent)(nil),
			(*types.MaterializedViewRefresh)(nil),
			(*types.DiscordBan)(nil),
			(*types.GroupMemberTracking)(nil),
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
			"game_tracking_users",
			"game_trackings",
			"outfit_asset_tracking_outfits",
			"outfit_asset_trackings",
			"group_member_tracking_users",
			"group_member_trackings",
			"condo_games",
			"group_mixed_classifications",
			"group_verifications",
			"group_reasons",
			"groups",
			// "user_badges",
			"user_favorites",
			"inventory_infos",
			"user_inventories",
			"game_infos",
			"user_games",
			"friend_infos",
			"user_friends",
			"asset_infos",
			"outfit_assets",
			"outfit_infos",
			"user_outfits",
			"group_infos",
			"user_groups",
			"user_clearances",
			"user_verifications",
			"user_reasons",
			"users",
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

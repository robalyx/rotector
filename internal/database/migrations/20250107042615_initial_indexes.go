package migrations

import (
	"context"
	"fmt"

	"github.com/robalyx/rotector/internal/database/types/enum"
	"github.com/uptrace/bun"
)

func init() { //nolint:funlen
	Migrations.MustRegister(func(ctx context.Context, db *bun.DB) error {
		_, err := db.NewRaw(`
			-- User activity logs indexes
			CREATE INDEX IF NOT EXISTS idx_activity_logs_time 
			ON activity_logs (activity_timestamp DESC, sequence DESC);

			CREATE INDEX IF NOT EXISTS idx_activity_logs_user_time 
			ON activity_logs (user_id, activity_timestamp DESC, sequence DESC) 
			WHERE user_id > 0;
			
			CREATE INDEX IF NOT EXISTS idx_activity_logs_group_time 
			ON activity_logs (group_id, activity_timestamp DESC, sequence DESC) 
			WHERE group_id > 0;
			
			CREATE INDEX IF NOT EXISTS idx_activity_logs_user_viewed 
			ON activity_logs (reviewer_id, activity_timestamp DESC, activity_type, user_id)
			WHERE user_id > 0 AND activity_type = ?;

			CREATE INDEX IF NOT EXISTS idx_activity_logs_group_viewed 
			ON activity_logs (reviewer_id, activity_timestamp DESC, activity_type, group_id)
			WHERE group_id > 0 AND activity_type = ?;
			
			CREATE INDEX IF NOT EXISTS idx_activity_logs_reviewer_time 
			ON activity_logs (reviewer_id, activity_timestamp DESC, sequence DESC);
			
			CREATE INDEX IF NOT EXISTS idx_activity_logs_type_time 
			ON activity_logs (activity_type, activity_timestamp DESC, sequence DESC);
			
			-- Group tracking indexes
			CREATE INDEX IF NOT EXISTS idx_group_member_trackings_check 
			ON group_member_trackings (last_checked ASC)
			WHERE is_flagged = false;

			CREATE INDEX IF NOT EXISTS idx_group_member_tracking_users_group
			ON group_member_tracking_users (group_id);

			CREATE INDEX IF NOT EXISTS idx_group_member_tracking_users_user
			ON group_member_tracking_users (user_id);

			-- Outfit asset tracking indexes
			CREATE INDEX IF NOT EXISTS idx_outfit_asset_trackings_check 
			ON outfit_asset_trackings (last_checked ASC)
			WHERE is_flagged = false;

			CREATE INDEX IF NOT EXISTS idx_outfit_asset_tracking_outfits_asset
			ON outfit_asset_tracking_outfits (asset_id);

			CREATE INDEX IF NOT EXISTS idx_outfit_asset_tracking_outfits_user
			ON outfit_asset_tracking_outfits (tracked_id)
			WHERE is_user_id = true;

			CREATE INDEX IF NOT EXISTS idx_outfit_asset_tracking_outfits_outfit_only
			ON outfit_asset_tracking_outfits (tracked_id)
			WHERE is_user_id = false;

			-- Game tracking indexes
			CREATE INDEX IF NOT EXISTS idx_game_trackings_check 
			ON game_trackings (last_checked ASC)
			WHERE is_flagged = false;

			CREATE INDEX IF NOT EXISTS idx_game_tracking_users_game
			ON game_tracking_users (game_id);

			CREATE INDEX IF NOT EXISTS idx_game_tracking_users_user
			ON game_tracking_users (user_id);

			-- Group review sorting indexes
			CREATE INDEX IF NOT EXISTS idx_groups_confidence
			ON groups (status, confidence DESC, last_viewed ASC);

			CREATE INDEX IF NOT EXISTS idx_groups_updated
			ON groups (status, last_updated ASC, last_viewed ASC);
			
			CREATE INDEX IF NOT EXISTS idx_groups_recently_updated
			ON groups (status, last_updated DESC, last_viewed ASC);

			CREATE INDEX IF NOT EXISTS idx_groups_viewed
			ON groups (status, last_viewed ASC);

			CREATE INDEX IF NOT EXISTS idx_groups_scan_time 
			ON groups (status, is_locked, last_scanned ASC, confidence DESC);

			CREATE INDEX IF NOT EXISTS idx_groups_lock_check 
			ON groups (status, last_lock_check ASC);

			CREATE INDEX IF NOT EXISTS idx_groups_thumbnail_update 
			ON groups (status, is_deleted, last_thumbnail_update ASC)
			WHERE is_deleted = false;
					
			CREATE INDEX IF NOT EXISTS idx_groups_locked_status
			ON groups (is_locked, status)
			WHERE is_locked = true;

			CREATE INDEX IF NOT EXISTS idx_groups_uuid
			ON groups (uuid);

			CREATE INDEX IF NOT EXISTS idx_groups_status_count
			ON groups (status);

			-- Group clearance index
			CREATE INDEX IF NOT EXISTS idx_group_clearances_cleared_at
			ON group_clearances (cleared_at);

			-- Group verification index
			CREATE INDEX IF NOT EXISTS idx_group_verifications_verified_at
			ON group_verifications (verified_at);

			-- User review sorting indexes
			CREATE INDEX IF NOT EXISTS idx_users_confidence
			ON users (status, confidence DESC, last_viewed ASC);

			CREATE INDEX IF NOT EXISTS idx_users_updated
			ON users (status, last_updated ASC, last_viewed ASC);
			
			CREATE INDEX IF NOT EXISTS idx_users_recently_updated
			ON users (status, last_updated DESC, last_viewed ASC);

			CREATE INDEX IF NOT EXISTS idx_users_viewed
			ON users (status, last_viewed ASC);

			CREATE INDEX IF NOT EXISTS idx_users_scan_time 
			ON users (status, is_banned, last_scanned ASC, confidence DESC);

			CREATE INDEX IF NOT EXISTS idx_users_ban_check 
			ON users (status, last_ban_check ASC);

			CREATE INDEX IF NOT EXISTS idx_users_thumbnail_update 
			ON users (status, is_deleted, last_thumbnail_update ASC)
			WHERE is_deleted = false;

			CREATE INDEX IF NOT EXISTS idx_users_banned_status
			ON users (is_banned, status)
			WHERE is_banned = true;

			CREATE INDEX IF NOT EXISTS idx_users_uuid
			ON users (uuid);

			CREATE INDEX IF NOT EXISTS idx_users_status_count
			ON users (status);

			-- User last_updated index for delete-after-time command
			CREATE INDEX IF NOT EXISTS idx_users_last_updated
			ON users (last_updated ASC);

			-- Statistics indexes
            CREATE INDEX IF NOT EXISTS idx_hourly_stats_timestamp
            ON hourly_stats (timestamp DESC);

			-- Discord server member indexes
			CREATE INDEX IF NOT EXISTS idx_server_members_user_joined
			ON discord_server_members (user_id, joined_at DESC);
			
			CREATE INDEX IF NOT EXISTS idx_server_members_updated_at
			ON discord_server_members (updated_at);

			CREATE INDEX IF NOT EXISTS idx_server_members_user_id
			ON discord_server_members (user_id);

			-- Inappropriate messages indexes
			CREATE INDEX IF NOT EXISTS idx_inappropriate_messages_detected 
			ON inappropriate_messages (server_id, channel_id, detected_at DESC);
			
			CREATE INDEX IF NOT EXISTS idx_inappropriate_messages_user_detected
			ON inappropriate_messages (server_id, channel_id, user_id, detected_at DESC);

			CREATE INDEX IF NOT EXISTS idx_inappropriate_messages_user_id
			ON inappropriate_messages (user_id);

			CREATE INDEX IF NOT EXISTS idx_inappropriate_messages_user_servers
			ON inappropriate_messages (user_id, server_id);

			-- Inappropriate user summaries indexes
			CREATE INDEX IF NOT EXISTS idx_inappropriate_summaries_message_count
			ON inappropriate_user_summaries (message_count DESC);
			
			CREATE INDEX IF NOT EXISTS idx_inappropriate_summaries_last_detected
			ON inappropriate_user_summaries (last_detected DESC);

			-- Guild ban logs indexes
			CREATE INDEX IF NOT EXISTS idx_guild_ban_logs_time_id
			ON guild_ban_logs (timestamp DESC, id DESC);

			-- Condo player indexes
			CREATE INDEX IF NOT EXISTS idx_condo_players_blacklisted_url
			ON condo_players (thumbnail_url)
			WHERE is_blacklisted = false;

			-- Condo game indexes
			CREATE INDEX IF NOT EXISTS idx_condo_games_scan_time
			ON condo_games (is_deleted, last_scanned ASC)
			WHERE is_deleted = false;

			-- Discord user full scan indexes
			CREATE INDEX IF NOT EXISTS idx_discord_user_full_scans_last_scan
			ON discord_user_full_scans (last_scan ASC);

			-- Comments indexes
			CREATE INDEX IF NOT EXISTS idx_user_comments_target_created 
			ON user_comments (target_id, created_at DESC);

			CREATE INDEX IF NOT EXISTS idx_user_comments_target_commenter 
			ON user_comments (target_id, commenter_id);

			-- Group comments indexes
			CREATE INDEX IF NOT EXISTS idx_group_comments_target_created 
			ON group_comments (target_id, created_at DESC);

			CREATE INDEX IF NOT EXISTS idx_group_comments_target_commenter 
			ON group_comments (target_id, commenter_id);

			-- Ivan message indexes
			CREATE INDEX IF NOT EXISTS idx_ivan_messages_user_time
			ON ivan_messages (user_id, date_time ASC);

			CREATE INDEX IF NOT EXISTS idx_ivan_messages_multi_user
			ON ivan_messages (user_id ASC, date_time ASC);

			CREATE INDEX IF NOT EXISTS idx_ivan_messages_unchecked
			ON ivan_messages (user_id) 
			WHERE was_checked = false;

			-- Reviewer info indexes
			CREATE INDEX IF NOT EXISTS idx_reviewer_infos_updated_at
			ON reviewer_infos (updated_at);

			-- User reasons indexes
			CREATE INDEX IF NOT EXISTS idx_user_reasons_user_id
			ON user_reasons (user_id);

			CREATE INDEX IF NOT EXISTS idx_user_reasons_type_user
			ON user_reasons (reason_type, user_id);

			-- Group reasons indexes
			CREATE INDEX IF NOT EXISTS idx_group_reasons_group_id
			ON group_reasons (group_id);

			-- User relationship indexes
			CREATE INDEX IF NOT EXISTS idx_user_groups_user_id_rank
			ON user_groups (user_id, role_rank DESC);

			CREATE INDEX IF NOT EXISTS idx_user_groups_group_id
			ON user_groups (group_id);

			CREATE INDEX IF NOT EXISTS idx_user_outfits_user_id
			ON user_outfits (user_id);

			CREATE INDEX IF NOT EXISTS idx_user_outfits_outfit_id
			ON user_outfits (outfit_id);

			CREATE INDEX IF NOT EXISTS idx_outfit_assets_outfit_id
			ON outfit_assets (outfit_id);

			CREATE INDEX IF NOT EXISTS idx_outfit_assets_asset_id
			ON outfit_assets (asset_id);
			
			CREATE INDEX IF NOT EXISTS idx_user_assets_user_id
			ON user_assets (user_id);

			CREATE INDEX IF NOT EXISTS idx_user_assets_asset_id
			ON user_assets (asset_id);

			CREATE INDEX IF NOT EXISTS idx_user_friends_user_id
			ON user_friends (user_id);

			CREATE INDEX IF NOT EXISTS idx_user_friends_friend_id
			ON user_friends (friend_id);

			CREATE INDEX IF NOT EXISTS idx_user_games_user_id
			ON user_games (user_id);

			CREATE INDEX IF NOT EXISTS idx_user_games_game_id
			ON user_games (game_id);

			CREATE INDEX IF NOT EXISTS idx_user_inventories_user_id
			ON user_inventories (user_id);

			CREATE INDEX IF NOT EXISTS idx_user_inventories_inventory_id
			ON user_inventories (inventory_id);

			CREATE INDEX IF NOT EXISTS idx_user_favorites_user_id
			ON user_favorites (user_id);

			CREATE INDEX IF NOT EXISTS idx_user_favorites_game_id
			ON user_favorites (game_id);

			CREATE INDEX IF NOT EXISTS idx_game_infos_place_visits
			ON game_infos (place_visits DESC);

			CREATE INDEX IF NOT EXISTS idx_friend_infos_id_updated
			ON friend_infos (id, last_updated DESC);
		`, enum.ActivityTypeUserViewed, enum.ActivityTypeGroupViewed).Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to create indexes: %w", err)
		}

		return nil
	}, func(ctx context.Context, db *bun.DB) error {
		_, err := db.NewRaw(`
			-- User activity logs indexes
			DROP INDEX IF EXISTS idx_activity_logs_time;
			DROP INDEX IF EXISTS idx_activity_logs_user_time;
			DROP INDEX IF EXISTS idx_activity_logs_group_time;
			DROP INDEX IF EXISTS idx_activity_logs_user_viewed;
			DROP INDEX IF EXISTS idx_activity_logs_group_viewed;
			DROP INDEX IF EXISTS idx_activity_logs_reviewer_time;
			DROP INDEX IF EXISTS idx_activity_logs_type_time;

			-- Group tracking indexes
			DROP INDEX IF EXISTS idx_group_member_trackings_check;

			DROP INDEX IF EXISTS idx_group_member_tracking_users_group;
			DROP INDEX IF EXISTS idx_group_member_tracking_users_user;

			-- Outfit asset tracking indexes
			DROP INDEX IF EXISTS idx_outfit_asset_trackings_check;
			DROP INDEX IF EXISTS idx_outfit_asset_tracking_outfits_asset;
			DROP INDEX IF EXISTS idx_outfit_asset_tracking_outfits_user;
			DROP INDEX IF EXISTS idx_outfit_asset_tracking_outfits_outfit_only;

			-- Game tracking indexes
			DROP INDEX IF EXISTS idx_game_trackings_check;

			DROP INDEX IF EXISTS idx_game_tracking_users_game;
			DROP INDEX IF EXISTS idx_game_tracking_users_user;

			-- Group review sorting indexes
			DROP INDEX IF EXISTS idx_groups_confidence;
			DROP INDEX IF EXISTS idx_groups_updated;
			DROP INDEX IF EXISTS idx_groups_recently_updated;
			DROP INDEX IF EXISTS idx_groups_viewed;
			DROP INDEX IF EXISTS idx_groups_scan_time;
			DROP INDEX IF EXISTS idx_groups_lock_check;
			DROP INDEX IF EXISTS idx_groups_thumbnail_update;
			DROP INDEX IF EXISTS idx_groups_locked_status;
			DROP INDEX IF EXISTS idx_groups_uuid;
			DROP INDEX IF EXISTS idx_groups_status_count;
			DROP INDEX IF EXISTS idx_group_clearances_cleared_at;
			DROP INDEX IF EXISTS idx_group_verifications_verified_at;

			-- User review sorting indexes
			DROP INDEX IF EXISTS idx_users_confidence;
			DROP INDEX IF EXISTS idx_users_updated;
			DROP INDEX IF EXISTS idx_users_recently_updated;
			DROP INDEX IF EXISTS idx_users_viewed;
			DROP INDEX IF EXISTS idx_users_scan_time;
			DROP INDEX IF EXISTS idx_users_ban_check;
			DROP INDEX IF EXISTS idx_users_thumbnail_update;
			DROP INDEX IF EXISTS idx_users_banned_status;
			DROP INDEX IF EXISTS idx_users_uuid;
			DROP INDEX IF EXISTS idx_users_status_count;

			-- User last_updated index for delete-after-time command
			DROP INDEX IF EXISTS idx_users_last_updated;

			-- Statistics indexes
            DROP INDEX IF EXISTS idx_hourly_stats_timestamp;

			-- Discord server member indexes
			DROP INDEX IF EXISTS idx_server_members_user_joined;
			DROP INDEX IF EXISTS idx_server_members_updated_at;
			DROP INDEX IF EXISTS idx_server_members_user_id;

			-- Inappropriate messages indexes
			DROP INDEX IF EXISTS idx_inappropriate_messages_detected;
			DROP INDEX IF EXISTS idx_inappropriate_messages_user_detected;
			DROP INDEX IF EXISTS idx_inappropriate_messages_user_id;
			DROP INDEX IF EXISTS idx_inappropriate_messages_user_servers;

			-- Inappropriate user summaries indexes
			DROP INDEX IF EXISTS idx_inappropriate_summaries_message_count;
			DROP INDEX IF EXISTS idx_inappropriate_summaries_last_detected;

			-- Guild ban logs indexes
			DROP INDEX IF EXISTS idx_guild_ban_logs_time_id;

			-- Condo player indexes
			DROP INDEX IF EXISTS idx_condo_players_blacklisted_url;

			-- Condo game indexes
			DROP INDEX IF EXISTS idx_condo_games_scan_time;

			-- Discord user full scan indexes
			DROP INDEX IF EXISTS idx_discord_user_full_scans_last_scan;

			-- Comments indexes
			DROP INDEX IF EXISTS idx_user_comments_target_created;
			DROP INDEX IF EXISTS idx_user_comments_target_commenter;

			-- Group comments indexes
			DROP INDEX IF EXISTS idx_group_comments_target_created;
			DROP INDEX IF EXISTS idx_group_comments_target_commenter;

			-- Ivan message indexes
			DROP INDEX IF EXISTS idx_ivan_messages_user_time;
			DROP INDEX IF EXISTS idx_ivan_messages_multi_user;
			DROP INDEX IF EXISTS idx_ivan_messages_unchecked;

			-- Reviewer info indexes
			DROP INDEX IF EXISTS idx_reviewer_infos_updated_at;

			-- User reasons indexes
			DROP INDEX IF EXISTS idx_user_reasons_user_id;
			DROP INDEX IF EXISTS idx_user_reasons_type_user;

			-- Group reasons indexes
			DROP INDEX IF EXISTS idx_group_reasons_group_id;

			-- User relationship indexes
			DROP INDEX IF EXISTS idx_user_groups_user_id_rank;
			DROP INDEX IF EXISTS idx_user_outfits_user_id;
			DROP INDEX IF EXISTS idx_outfit_assets_outfit_id;
			DROP INDEX IF EXISTS idx_outfit_assets_asset_id;
			DROP INDEX IF EXISTS idx_user_assets_user_id;
			DROP INDEX IF EXISTS idx_user_assets_asset_id;
			DROP INDEX IF EXISTS idx_user_friends_user_id;
			DROP INDEX IF EXISTS idx_user_games_user_id;
			DROP INDEX IF EXISTS idx_user_inventories_user_id;
			DROP INDEX IF EXISTS idx_user_favorites_user_id;
			DROP INDEX IF EXISTS idx_user_favorites_game_id;

			-- User relationship info indexes
			DROP INDEX IF EXISTS idx_game_infos_place_visits;
			DROP INDEX IF EXISTS idx_friend_infos_id_updated;

			-- User relationship deletion indexes
			DROP INDEX IF EXISTS idx_user_groups_group_id;
			DROP INDEX IF EXISTS idx_user_outfits_outfit_id;
			DROP INDEX IF EXISTS idx_user_friends_friend_id;
			DROP INDEX IF EXISTS idx_user_games_game_id;
			DROP INDEX IF EXISTS idx_user_inventories_inventory_id;
		`).Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to drop indexes: %w", err)
		}

		return nil
	})
}

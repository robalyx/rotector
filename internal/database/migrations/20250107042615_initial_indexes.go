package migrations

import (
	"context"
	"fmt"

	"github.com/robalyx/rotector/internal/database/types/enum"
	"github.com/uptrace/bun"
)

func init() { //nolint:funlen
	periods := []struct {
		name     string
		interval string
	}{
		{enum.LeaderboardPeriodDaily.String(), "INTERVAL '1 day'"},
		{enum.LeaderboardPeriodWeekly.String(), "INTERVAL '1 week'"},
		{enum.LeaderboardPeriodBiWeekly.String(), "INTERVAL '2 weeks'"},
		{enum.LeaderboardPeriodMonthly.String(), "INTERVAL '1 month'"},
		{enum.LeaderboardPeriodBiAnnually.String(), "INTERVAL '6 months'"},
		{enum.LeaderboardPeriodAnnually.String(), "INTERVAL '1 year'"},
		{enum.LeaderboardPeriodAllTime.String(), "NULL"},
	}

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

			-- Appeal indexes
			CREATE INDEX IF NOT EXISTS idx_appeals_user_pending_type
			ON appeals (user_id, type) WHERE status = 0;
			CREATE INDEX IF NOT EXISTS idx_appeals_requester_pending_type
			ON appeals (requester_id, type) WHERE status = 0;
			CREATE INDEX IF NOT EXISTS idx_appeals_user_rejected_type
			ON appeals (user_id, type, claimed_at DESC) WHERE status = 2;
			CREATE INDEX IF NOT EXISTS idx_appeals_user_rejected_count
			ON appeals (user_id, status) WHERE status = 2;
			CREATE INDEX IF NOT EXISTS idx_appeals_claimed_by
			ON appeals (claimed_by) WHERE claimed_by > 0;
			CREATE INDEX IF NOT EXISTS idx_appeals_timestamp
			ON appeals (timestamp DESC);
			CREATE INDEX IF NOT EXISTS idx_appeals_status_timestamp
			ON appeals (status, timestamp DESC);
			CREATE INDEX IF NOT EXISTS idx_appeals_id_status
			ON appeals (id, status);

			-- Appeal timeline indexes
			CREATE INDEX IF NOT EXISTS idx_appeal_timelines_timestamp_asc 
			ON appeal_timelines (timestamp ASC, id ASC);

			CREATE INDEX IF NOT EXISTS idx_appeal_timelines_timestamp_desc 
			ON appeal_timelines (timestamp DESC, id DESC);

			CREATE INDEX IF NOT EXISTS idx_appeal_timelines_activity_desc
			ON appeal_timelines (last_activity DESC, id DESC);

			-- Appeal messages index
			CREATE INDEX IF NOT EXISTS idx_appeal_messages_appeal_created
			ON appeal_messages (appeal_id, created_at ASC);
			
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
			WHERE is_user_id = TRUE;

			CREATE INDEX IF NOT EXISTS idx_outfit_asset_tracking_outfits_outfit_only
			ON outfit_asset_tracking_outfits (tracked_id)
			WHERE is_user_id = FALSE;

			-- Group review sorting indexes
			CREATE INDEX IF NOT EXISTS idx_groups_confidence
			ON groups (status, confidence DESC, last_viewed ASC);

			CREATE INDEX IF NOT EXISTS idx_groups_updated
			ON groups (status, last_updated ASC, last_viewed ASC);

			CREATE INDEX IF NOT EXISTS idx_groups_viewed
			ON groups (status, last_viewed ASC);

			CREATE INDEX IF NOT EXISTS idx_groups_scan_time 
			ON groups (status, last_scanned ASC, confidence DESC);

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

			-- Group reputation sorting indexes
			CREATE INDEX IF NOT EXISTS idx_group_reputations_score
			ON group_reputations (score ASC);

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

			CREATE INDEX IF NOT EXISTS idx_users_viewed
			ON users (status, last_viewed ASC);

			CREATE INDEX IF NOT EXISTS idx_users_scan_time 
			ON users (status, last_scanned ASC, confidence DESC);

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

			-- User reputation sorting indexes
			CREATE INDEX IF NOT EXISTS idx_user_reputations_score
			ON user_reputations (score ASC);

			-- Statistics indexes
            CREATE INDEX IF NOT EXISTS idx_hourly_stats_timestamp
            ON hourly_stats (timestamp DESC);

			-- Vote indexes
			CREATE INDEX IF NOT EXISTS idx_user_votes_id_discord 
			ON user_votes (id, discord_user_id);
			
			CREATE INDEX IF NOT EXISTS idx_user_votes_verify
			ON user_votes (id, is_verified) 
			WHERE is_verified = false;
			
			CREATE INDEX IF NOT EXISTS idx_group_votes_id_discord 
			ON group_votes (id, discord_user_id);
			
			CREATE INDEX IF NOT EXISTS idx_group_votes_verify
			ON group_votes (id, is_verified)
			WHERE is_verified = false;

			-- Vote statistics index
			CREATE INDEX IF NOT EXISTS idx_vote_stats_voted_at 
			ON vote_stats (voted_at DESC);

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

			-- User relationship indexes
			CREATE INDEX IF NOT EXISTS idx_user_groups_user_id_rank
			ON user_groups (user_id, role_rank DESC);

			CREATE INDEX IF NOT EXISTS idx_user_outfits_user_id
			ON user_outfits (user_id);

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

			CREATE INDEX IF NOT EXISTS idx_user_games_user_id
			ON user_games (user_id);

			CREATE INDEX IF NOT EXISTS idx_user_inventory_user_id
			ON user_inventories (user_id);

			-- NOTE: We will make use of these in the future
			-- CREATE INDEX IF NOT EXISTS idx_user_favorites_user_id
			-- ON user_favorites (user_id);
			-- CREATE INDEX IF NOT EXISTS idx_user_badges_user_id
			-- ON user_badges (user_id);

			-- User relationship info indexes
			CREATE INDEX IF NOT EXISTS idx_game_infos_place_visits
			ON game_infos (place_visits DESC);

			-- User relationship deletion indexes
			CREATE INDEX IF NOT EXISTS idx_user_groups_group_id
			ON user_groups (group_id);

			CREATE INDEX IF NOT EXISTS idx_user_outfits_outfit_id
			ON user_outfits (outfit_id);

			CREATE INDEX IF NOT EXISTS idx_user_friends_friend_id
			ON user_friends (friend_id);

			CREATE INDEX IF NOT EXISTS idx_user_games_game_id
			ON user_games (game_id);

			CREATE INDEX IF NOT EXISTS idx_user_inventory_inventory_id
			ON user_inventories (inventory_id);
		`, enum.ActivityTypeUserViewed, enum.ActivityTypeGroupViewed).Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to create indexes: %w", err)
		}

		// Vote leaderboard indexes
		for _, period := range periods {
			viewName := "vote_leaderboard_stats_" + period.name

			// Unique index for concurrent operations
			_, err = db.NewRaw(fmt.Sprintf(`
				CREATE UNIQUE INDEX IF NOT EXISTS idx_vote_leaderboard_%s_unique
				ON %s (discord_user_id);
			`, period.name, viewName)).Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to create unique index for %s: %w", period.name, err)
			}

			// Index for sorting and pagination
			_, err = db.NewRaw(fmt.Sprintf(`
				CREATE INDEX IF NOT EXISTS idx_vote_leaderboard_%s_sort
				ON %s (
					correct_votes DESC,
					accuracy DESC, 
					voted_at DESC,
					discord_user_id
				) INCLUDE (total_votes);
			`, period.name, viewName)).Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to create sort index for %s: %w", period.name, err)
			}

			// Index for user lookups
			_, err = db.NewRaw(fmt.Sprintf(`
				CREATE INDEX IF NOT EXISTS idx_vote_leaderboard_%s_user
				ON %s (discord_user_id)
				INCLUDE (correct_votes, total_votes, accuracy);
			`, period.name, viewName)).Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to create user index for %s: %w", period.name, err)
			}
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

			-- Appeal indexes
			DROP INDEX IF EXISTS idx_appeals_user_pending_type;
			DROP INDEX IF EXISTS idx_appeals_requester_pending_type;
			DROP INDEX IF EXISTS idx_appeals_user_rejected_type;
			DROP INDEX IF EXISTS idx_appeals_user_rejected_count;
			DROP INDEX IF EXISTS idx_appeals_claimed_by;
			DROP INDEX IF EXISTS idx_appeals_timestamp;
			DROP INDEX IF EXISTS idx_appeals_status_timestamp;
			DROP INDEX IF EXISTS idx_appeals_id_status;

			-- Appeal timeline indexes
			DROP INDEX IF EXISTS idx_appeal_timelines_timestamp_asc;
			DROP INDEX IF EXISTS idx_appeal_timelines_timestamp_desc;
			DROP INDEX IF EXISTS idx_appeal_timelines_activity_desc;

			-- Appeal messages index
			DROP INDEX IF EXISTS idx_appeal_messages_appeal_created;

			-- Group tracking indexes
			DROP INDEX IF EXISTS idx_group_member_trackings_check;

			DROP INDEX IF EXISTS idx_group_member_tracking_users_group;
			DROP INDEX IF EXISTS idx_group_member_tracking_users_user;

			-- Outfit asset tracking indexes
			DROP INDEX IF EXISTS idx_outfit_asset_trackings_check;
			DROP INDEX IF EXISTS idx_outfit_asset_tracking_outfits_asset;
			DROP INDEX IF EXISTS idx_outfit_asset_tracking_outfits_user;
			DROP INDEX IF EXISTS idx_outfit_asset_tracking_outfits_outfit_only;

			-- Group review sorting indexes
			DROP INDEX IF EXISTS idx_groups_confidence;
			DROP INDEX IF EXISTS idx_groups_updated;
			DROP INDEX IF EXISTS idx_groups_viewed;
			DROP INDEX IF EXISTS idx_groups_scan_time;
			DROP INDEX IF EXISTS idx_groups_lock_check;
			DROP INDEX IF EXISTS idx_groups_thumbnail_update;
			DROP INDEX IF EXISTS idx_groups_locked_status;
			DROP INDEX IF EXISTS idx_group_clearances_cleared_at;
			DROP INDEX IF EXISTS idx_group_verifications_verified_at;

			-- User review sorting indexes
			DROP INDEX IF EXISTS idx_users_confidence;
			DROP INDEX IF EXISTS idx_users_updated;
			DROP INDEX IF EXISTS idx_users_viewed;
			DROP INDEX IF EXISTS idx_users_scan_time;
			DROP INDEX IF EXISTS idx_users_ban_check;
			DROP INDEX IF EXISTS idx_users_thumbnail_update;
			DROP INDEX IF EXISTS idx_users_banned_status;
			DROP INDEX IF EXISTS idx_users_uuid;
			DROP INDEX IF EXISTS idx_users_status_count;

			-- User reputation sorting indexes
			DROP INDEX IF EXISTS idx_user_reputations_score;

			-- Group review sorting indexes
			DROP INDEX IF EXISTS idx_groups_confidence;
			DROP INDEX IF EXISTS idx_groups_updated;
			DROP INDEX IF EXISTS idx_groups_viewed;
			DROP INDEX IF EXISTS idx_groups_scan_time;
			DROP INDEX IF EXISTS idx_groups_lock_check;
			DROP INDEX IF EXISTS idx_groups_thumbnail_update;
			DROP INDEX IF EXISTS idx_groups_locked_status;
			DROP INDEX IF EXISTS idx_groups_uuid;
			DROP INDEX IF EXISTS idx_groups_status_count;

			-- Group reputation sorting indexes
			DROP INDEX IF EXISTS idx_group_reputations_score;

			-- Statistics indexes
            DROP INDEX IF EXISTS idx_hourly_stats_timestamp;

			-- Vote indexes
			DROP INDEX IF EXISTS idx_user_votes_id_discord;
			DROP INDEX IF EXISTS idx_user_votes_verify;
			DROP INDEX IF EXISTS idx_group_votes_id_discord;
			DROP INDEX IF EXISTS idx_group_votes_verify;

			-- Vote statistics index
			DROP INDEX IF EXISTS idx_vote_stats_voted_at;

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

			-- User relationship indexes
			DROP INDEX IF EXISTS idx_user_groups_user_id_rank;
			DROP INDEX IF EXISTS idx_user_outfits_user_id;
			DROP INDEX IF EXISTS idx_outfit_assets_outfit_id;
			DROP INDEX IF EXISTS idx_outfit_assets_asset_id;
			DROP INDEX IF EXISTS idx_user_assets_user_id;
			DROP INDEX IF EXISTS idx_user_assets_asset_id;
			DROP INDEX IF EXISTS idx_user_friends_user_id;
			DROP INDEX IF EXISTS idx_user_games_user_id;
			DROP INDEX IF EXISTS idx_user_inventory_user_id;
			-- NOTE: We will make use of these in the future
			-- DROP INDEX IF EXISTS idx_user_favorites_user_id;
			-- DROP INDEX IF EXISTS idx_user_badges_user_id;

			-- User relationship info indexes
			DROP INDEX IF EXISTS idx_game_infos_place_visits;

			-- User relationship deletion indexes
			DROP INDEX IF EXISTS idx_user_groups_group_id;
			DROP INDEX IF EXISTS idx_user_outfits_outfit_id;
			DROP INDEX IF EXISTS idx_user_friends_friend_id;
			DROP INDEX IF EXISTS idx_user_games_game_id;
			DROP INDEX IF EXISTS idx_user_inventory_inventory_id;
		`).Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to drop indexes: %w", err)
		}

		// Drop vote leaderboard indexes
		for _, period := range periods {
			_, err = db.NewRaw(fmt.Sprintf(`
				DROP INDEX IF EXISTS idx_vote_leaderboard_%s_unique;
				DROP INDEX IF EXISTS idx_vote_leaderboard_%s_sort;
				DROP INDEX IF EXISTS idx_vote_leaderboard_%s_user;
			`, period.name, period.name, period.name)).Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to drop indexes for %s: %w", period.name, err)
			}
		}

		return nil
	})
}

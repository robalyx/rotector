package migrations

import (
	"context"
	"fmt"

	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
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

			-- Scanning indexes for users and groups
			CREATE INDEX IF NOT EXISTS idx_confirmed_users_scan_time 
			ON confirmed_users (last_scanned ASC, confidence DESC);
			CREATE INDEX IF NOT EXISTS idx_flagged_users_scan_time 
			ON flagged_users (confidence DESC, last_scanned ASC)
			WHERE confidence >= 0.8;
			CREATE INDEX IF NOT EXISTS idx_confirmed_groups_scan_time 
			ON confirmed_groups (last_scanned ASC, confidence DESC);
			CREATE INDEX IF NOT EXISTS idx_flagged_groups_scan_time 
			ON flagged_groups (last_scanned ASC, confidence DESC);
			
			-- Group tracking indexes
			CREATE INDEX IF NOT EXISTS idx_group_member_trackings_check 
			ON group_member_trackings (cardinality(flagged_users) DESC, last_checked ASC)
			WHERE is_flagged = false;
			
			CREATE INDEX IF NOT EXISTS idx_group_member_trackings_id
			ON group_member_trackings (id);
			
			CREATE INDEX IF NOT EXISTS idx_group_member_trackings_cleanup
			ON group_member_trackings (last_appended)
			WHERE is_flagged = false;
			
			-- Group review sorting indexes
			CREATE INDEX IF NOT EXISTS idx_flagged_groups_confidence
			ON flagged_groups (confidence DESC, last_viewed ASC);
			CREATE INDEX IF NOT EXISTS idx_confirmed_groups_confidence
			ON confirmed_groups (confidence DESC, last_viewed ASC);
			CREATE INDEX IF NOT EXISTS idx_cleared_groups_confidence
			ON cleared_groups (confidence DESC, last_viewed ASC);

			CREATE INDEX IF NOT EXISTS idx_flagged_groups_updated
			ON flagged_groups (last_updated ASC, last_viewed ASC);
			CREATE INDEX IF NOT EXISTS idx_confirmed_groups_updated
			ON confirmed_groups (last_updated ASC, last_viewed ASC);
			CREATE INDEX IF NOT EXISTS idx_cleared_groups_updated
			ON cleared_groups (last_updated ASC, last_viewed ASC);

			-- User review sorting indexes
			CREATE INDEX IF NOT EXISTS idx_flagged_users_confidence
			ON flagged_users (confidence DESC, last_viewed ASC);
			CREATE INDEX IF NOT EXISTS idx_confirmed_users_confidence
			ON confirmed_users (confidence DESC, last_viewed ASC);
			CREATE INDEX IF NOT EXISTS idx_cleared_users_confidence
			ON cleared_users (confidence DESC, last_viewed ASC);

			CREATE INDEX IF NOT EXISTS idx_flagged_users_updated
			ON flagged_users (last_updated ASC, last_viewed ASC);
			CREATE INDEX IF NOT EXISTS idx_confirmed_users_updated
			ON confirmed_users (last_updated ASC, last_viewed ASC);
			CREATE INDEX IF NOT EXISTS idx_cleared_users_updated
			ON cleared_users (last_updated ASC, last_viewed ASC);

			-- User reputation sorting indexes
			CREATE INDEX IF NOT EXISTS idx_user_reputations_score_viewed
			ON user_reputations (score ASC)
			INCLUDE (id);

			CREATE INDEX IF NOT EXISTS idx_flagged_users_last_viewed
			ON flagged_users (last_viewed ASC)
			INCLUDE (id);
			CREATE INDEX IF NOT EXISTS idx_confirmed_users_last_viewed
			ON confirmed_users (last_viewed ASC)
			INCLUDE (id);
			CREATE INDEX IF NOT EXISTS idx_cleared_users_last_viewed
			ON cleared_users (last_viewed ASC)
			INCLUDE (id);

			-- Group reputation sorting indexes
			CREATE INDEX IF NOT EXISTS idx_group_reputations_score_viewed
			ON group_reputations (score ASC)
			INCLUDE (id);

			CREATE INDEX IF NOT EXISTS idx_flagged_groups_last_viewed
			ON flagged_groups (last_viewed ASC)
			INCLUDE (id);
			CREATE INDEX IF NOT EXISTS idx_confirmed_groups_last_viewed
			ON confirmed_groups (last_viewed ASC)
			INCLUDE (id);
			CREATE INDEX IF NOT EXISTS idx_cleared_groups_last_viewed
			ON cleared_groups (last_viewed ASC)
			INCLUDE (id);

			-- User thumbnail update indexes
			CREATE INDEX IF NOT EXISTS idx_flagged_users_thumbnail_update 
			ON flagged_users (is_deleted, last_thumbnail_update ASC)
			WHERE is_deleted = false;
			CREATE INDEX IF NOT EXISTS idx_confirmed_users_thumbnail_update 
			ON confirmed_users (is_deleted, last_thumbnail_update ASC)
			WHERE is_deleted = false;
			CREATE INDEX IF NOT EXISTS idx_cleared_users_thumbnail_update 
			ON cleared_users (is_deleted, last_thumbnail_update ASC)
			WHERE is_deleted = false;

			-- Group thumbnail update indexes
			CREATE INDEX IF NOT EXISTS idx_flagged_groups_thumbnail_update 
			ON flagged_groups (is_deleted, last_thumbnail_update ASC)
			WHERE is_deleted = false;
			CREATE INDEX IF NOT EXISTS idx_confirmed_groups_thumbnail_update 
			ON confirmed_groups (is_deleted, last_thumbnail_update ASC)
			WHERE is_deleted = false;
			CREATE INDEX IF NOT EXISTS idx_cleared_groups_thumbnail_update 
			ON cleared_groups (is_deleted, last_thumbnail_update ASC)
			WHERE is_deleted = false;

			-- User status indexes
			CREATE INDEX IF NOT EXISTS idx_cleared_users_purged_at
			ON cleared_users (cleared_at);
			CREATE INDEX IF NOT EXISTS idx_flagged_users_ban_check 
			ON flagged_users (last_ban_check ASC);
			CREATE INDEX IF NOT EXISTS idx_confirmed_users_ban_check 
			ON confirmed_users (last_ban_check ASC);
			
			-- Group status indexes
			CREATE INDEX IF NOT EXISTS idx_cleared_groups_purged_at
			ON cleared_groups (cleared_at);
			CREATE INDEX IF NOT EXISTS idx_flagged_groups_lock_check 
			ON flagged_groups (last_lock_check ASC);
			CREATE INDEX IF NOT EXISTS idx_confirmed_groups_lock_check 
			ON confirmed_groups (last_lock_check ASC);

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

			-- Scanning indexes
			DROP INDEX IF EXISTS idx_confirmed_users_scan_time;
			DROP INDEX IF EXISTS idx_flagged_users_scan_time;
			DROP INDEX IF EXISTS idx_confirmed_groups_scan_time;
			DROP INDEX IF EXISTS idx_flagged_groups_scan_time;

			-- Group tracking indexes
			DROP INDEX IF EXISTS idx_group_member_trackings_check;
			DROP INDEX IF EXISTS idx_group_member_trackings_id;
			DROP INDEX IF EXISTS idx_group_member_trackings_cleanup;

			-- Group review sorting indexes
			DROP INDEX IF EXISTS idx_flagged_groups_confidence;
			DROP INDEX IF EXISTS idx_confirmed_groups_confidence;
			DROP INDEX IF EXISTS idx_cleared_groups_confidence;

			DROP INDEX IF EXISTS idx_flagged_groups_updated;
			DROP INDEX IF EXISTS idx_confirmed_groups_updated;
			DROP INDEX IF EXISTS idx_cleared_groups_updated;

			DROP INDEX IF EXISTS idx_group_reputations_id_score;

			-- User review sorting indexes
			DROP INDEX IF EXISTS idx_flagged_users_confidence;
			DROP INDEX IF EXISTS idx_confirmed_users_confidence;
			DROP INDEX IF EXISTS idx_cleared_users_confidence;

			DROP INDEX IF EXISTS idx_flagged_users_updated;
			DROP INDEX IF EXISTS idx_confirmed_users_updated;
			DROP INDEX IF EXISTS idx_cleared_users_updated;

			DROP INDEX IF EXISTS idx_user_reputations_id_score;

			-- User reputation sorting indexes
			DROP INDEX IF EXISTS idx_user_reputations_score_viewed;
			DROP INDEX IF EXISTS idx_flagged_users_last_viewed;
			DROP INDEX IF EXISTS idx_confirmed_users_last_viewed;
			DROP INDEX IF EXISTS idx_cleared_users_last_viewed;

			-- Group reputation sorting indexes
			DROP INDEX IF EXISTS idx_group_reputations_score_viewed;
			DROP INDEX IF EXISTS idx_flagged_groups_last_viewed;
			DROP INDEX IF EXISTS idx_confirmed_groups_last_viewed;
			DROP INDEX IF EXISTS idx_cleared_groups_last_viewed;

			-- User status indexes
			DROP INDEX IF EXISTS idx_cleared_users_purged_at;
			DROP INDEX IF EXISTS idx_flagged_users_ban_check;
			DROP INDEX IF EXISTS idx_confirmed_users_ban_check;

			-- Group status indexes
			DROP INDEX IF EXISTS idx_cleared_groups_purged_at;
			DROP INDEX IF EXISTS idx_flagged_groups_lock_check;
			DROP INDEX IF EXISTS idx_confirmed_groups_lock_check;

			-- User thumbnail update indexes
			DROP INDEX IF EXISTS idx_flagged_users_thumbnail_update;
			DROP INDEX IF EXISTS idx_confirmed_users_thumbnail_update;
			DROP INDEX IF EXISTS idx_cleared_users_thumbnail_update;

			-- Group thumbnail update indexes
			DROP INDEX IF EXISTS idx_flagged_groups_thumbnail_update;
			DROP INDEX IF EXISTS idx_confirmed_groups_thumbnail_update;
			DROP INDEX IF EXISTS idx_cleared_groups_thumbnail_update;

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

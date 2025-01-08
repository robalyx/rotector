package migrations

import (
	"context"
	"fmt"

	"github.com/rotector/rotector/internal/common/storage/database/types"
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

			-- Appeal indexes
			CREATE INDEX IF NOT EXISTS idx_appeals_user_id ON appeals (user_id);
			CREATE INDEX IF NOT EXISTS idx_appeals_requester_id ON appeals (requester_id);
			CREATE INDEX IF NOT EXISTS idx_appeals_status ON appeals (status);
			CREATE INDEX IF NOT EXISTS idx_appeals_claimed_by ON appeals (claimed_by) WHERE claimed_by > 0;
			CREATE INDEX IF NOT EXISTS idx_appeals_rejected_reviewed_at ON appeals (reviewed_at DESC);

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
			CREATE INDEX IF NOT EXISTS idx_confirmed_users_scan_time ON confirmed_users (last_scanned ASC);
			CREATE INDEX IF NOT EXISTS idx_flagged_users_scan_time ON flagged_users (last_scanned ASC);
			CREATE INDEX IF NOT EXISTS idx_confirmed_groups_scan_time ON confirmed_groups (last_scanned ASC);
			CREATE INDEX IF NOT EXISTS idx_flagged_groups_scan_time ON flagged_groups (last_scanned ASC);
			
			-- Group tracking indexes
			CREATE INDEX IF NOT EXISTS idx_group_member_trackings_check 
			ON group_member_trackings (is_flagged, (cardinality(flagged_users)), last_checked ASC)
			WHERE is_flagged = false;
			
			CREATE INDEX IF NOT EXISTS idx_group_member_trackings_group_id
			ON group_member_trackings (group_id);
			
			CREATE INDEX IF NOT EXISTS idx_group_member_trackings_cleanup
			ON group_member_trackings (last_appended)
			WHERE is_flagged = false;
			
			-- Group review sorting indexes
			CREATE INDEX IF NOT EXISTS idx_flagged_groups_confidence_viewed 
			ON flagged_groups (confidence DESC, last_viewed ASC);
			CREATE INDEX IF NOT EXISTS idx_confirmed_groups_confidence_viewed 
			ON confirmed_groups (confidence DESC, last_viewed ASC);
			CREATE INDEX IF NOT EXISTS idx_cleared_groups_confidence_viewed 
			ON cleared_groups (confidence DESC, last_viewed ASC);
			CREATE INDEX IF NOT EXISTS idx_locked_groups_confidence_viewed 
			ON locked_groups (confidence DESC, last_viewed ASC);

			CREATE INDEX IF NOT EXISTS idx_flagged_groups_updated_viewed 
			ON flagged_groups (last_updated ASC, last_viewed ASC);
			CREATE INDEX IF NOT EXISTS idx_confirmed_groups_updated_viewed 
			ON confirmed_groups (last_updated ASC, last_viewed ASC);
			CREATE INDEX IF NOT EXISTS idx_cleared_groups_updated_viewed 
			ON cleared_groups (last_updated ASC, last_viewed ASC);
			CREATE INDEX IF NOT EXISTS idx_locked_groups_updated_viewed 
			ON locked_groups (last_updated ASC, last_viewed ASC);
			
			CREATE INDEX IF NOT EXISTS idx_group_reputations_id_score 
			ON group_reputations (id, score);

			-- User review sorting indexes
			CREATE INDEX IF NOT EXISTS idx_flagged_users_confidence_viewed 
			ON flagged_users (confidence DESC, last_viewed ASC);
			CREATE INDEX IF NOT EXISTS idx_confirmed_users_confidence_viewed 
			ON confirmed_users (confidence DESC, last_viewed ASC);
			CREATE INDEX IF NOT EXISTS idx_cleared_users_confidence_viewed 
			ON cleared_users (confidence DESC, last_viewed ASC);
			CREATE INDEX IF NOT EXISTS idx_banned_users_confidence_viewed 
			ON banned_users (confidence DESC, last_viewed ASC);
			
			CREATE INDEX IF NOT EXISTS idx_flagged_users_updated_viewed 
			ON flagged_users (last_updated ASC, last_viewed ASC);
			CREATE INDEX IF NOT EXISTS idx_confirmed_users_updated_viewed 
			ON confirmed_users (last_updated ASC, last_viewed ASC);
			CREATE INDEX IF NOT EXISTS idx_cleared_users_updated_viewed 
			ON cleared_users (last_updated ASC, last_viewed ASC);
			CREATE INDEX IF NOT EXISTS idx_banned_users_updated_viewed 
			ON banned_users (last_updated ASC, last_viewed ASC);

			CREATE INDEX IF NOT EXISTS idx_user_reputations_id_score 
			ON user_reputations (id, score);
			
			-- User status indexes
			CREATE INDEX IF NOT EXISTS idx_cleared_users_purged_at ON cleared_users (cleared_at);
			CREATE INDEX IF NOT EXISTS idx_flagged_users_last_purge_check ON flagged_users (last_purge_check);
			CREATE INDEX IF NOT EXISTS idx_confirmed_users_last_purge_check ON confirmed_users (last_purge_check);
			
			-- Group status indexes
			CREATE INDEX IF NOT EXISTS idx_cleared_groups_purged_at ON cleared_groups (cleared_at);
			CREATE INDEX IF NOT EXISTS idx_flagged_groups_last_purge_check ON flagged_groups (last_purge_check);
			CREATE INDEX IF NOT EXISTS idx_confirmed_groups_last_purge_check ON confirmed_groups (last_purge_check);
			
			-- Statistics indexes
			CREATE INDEX IF NOT EXISTS idx_hourly_stats_timestamp ON hourly_stats (timestamp DESC);
		`, types.ActivityTypeUserViewed, types.ActivityTypeGroupViewed).Exec(ctx)
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

			-- Appeal indexes
			DROP INDEX IF EXISTS idx_appeals_user_id;
			DROP INDEX IF EXISTS idx_appeals_requester_id;
			DROP INDEX IF EXISTS idx_appeals_status;
			DROP INDEX IF EXISTS idx_appeals_claimed_by;
			DROP INDEX IF EXISTS idx_appeals_rejected_reviewed_at;

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
			DROP INDEX IF EXISTS idx_group_member_trackings_group_id;
			DROP INDEX IF EXISTS idx_group_member_trackings_cleanup;

			-- Group review sorting indexes
			DROP INDEX IF EXISTS idx_flagged_groups_confidence_viewed;
			DROP INDEX IF EXISTS idx_confirmed_groups_confidence_viewed;
			DROP INDEX IF EXISTS idx_cleared_groups_confidence_viewed;
			DROP INDEX IF EXISTS idx_locked_groups_confidence_viewed;

			DROP INDEX IF EXISTS idx_flagged_groups_updated_viewed;
			DROP INDEX IF EXISTS idx_confirmed_groups_updated_viewed;
			DROP INDEX IF EXISTS idx_cleared_groups_updated_viewed;
			DROP INDEX IF EXISTS idx_locked_groups_updated_viewed;

			DROP INDEX IF EXISTS idx_group_reputations_id_score;

			-- User review sorting indexes
			DROP INDEX IF EXISTS idx_flagged_users_confidence_viewed;
			DROP INDEX IF EXISTS idx_confirmed_users_confidence_viewed;
			DROP INDEX IF EXISTS idx_cleared_users_confidence_viewed;
			DROP INDEX IF EXISTS idx_banned_users_confidence_viewed;

			DROP INDEX IF EXISTS idx_flagged_users_updated_viewed;
			DROP INDEX IF EXISTS idx_confirmed_users_updated_viewed;
			DROP INDEX IF EXISTS idx_cleared_users_updated_viewed;
			DROP INDEX IF EXISTS idx_banned_users_updated_viewed;

			DROP INDEX IF EXISTS idx_user_reputations_id_score;

			-- User status indexes
			DROP INDEX IF EXISTS idx_cleared_users_purged_at;
			DROP INDEX IF EXISTS idx_flagged_users_last_purge_check;
			DROP INDEX IF EXISTS idx_confirmed_users_last_purge_check;

			-- Group status indexes
			DROP INDEX IF EXISTS idx_cleared_groups_purged_at;
			DROP INDEX IF EXISTS idx_flagged_groups_last_purge_check;
			DROP INDEX IF EXISTS idx_confirmed_groups_last_purge_check;

			-- Statistics indexes
			DROP INDEX IF EXISTS idx_hourly_stats_timestamp;
		`).Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to drop indexes: %w", err)
		}

		return nil
	})
}

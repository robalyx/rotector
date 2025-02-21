package migrations

import (
	"context"
	"fmt"
	"strings"

	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
	"github.com/uptrace/bun"
)

func init() {
	periods := []struct {
		name     string
		interval string
	}{
		{enum.ReviewerStatsPeriodDaily.String(), "INTERVAL '1 day'"},
		{enum.ReviewerStatsPeriodWeekly.String(), "INTERVAL '1 week'"},
		{enum.ReviewerStatsPeriodMonthly.String(), "INTERVAL '1 month'"},
	}

	Migrations.MustRegister(func(ctx context.Context, db *bun.DB) error {
		// Create materialized views for reviewer stats
		for _, period := range periods {
			query := fmt.Sprintf(`
                CREATE MATERIALIZED VIEW IF NOT EXISTS reviewer_stats_%s AS
                WITH reviewer_activities AS (
                    SELECT 
                        reviewer_id,
                        COUNT(*) FILTER (WHERE activity_type = %d) as users_viewed,
                        COUNT(*) FILTER (WHERE activity_type = %d) as users_confirmed,
                        COUNT(*) FILTER (WHERE activity_type = %d) as users_cleared,
                        MAX(activity_timestamp) as last_activity
                    FROM activity_logs
                    WHERE 
                        activity_timestamp > NOW() - %s
                        AND reviewer_id > 0
                        AND activity_type IN (%d, %d, %d)
                    GROUP BY reviewer_id
                )
                SELECT 
                    reviewer_id,
                    COALESCE(users_viewed, 0) as users_viewed,
                    COALESCE(users_confirmed, 0) as users_confirmed,
                    COALESCE(users_cleared, 0) as users_cleared,
                    COALESCE(last_activity, '1970-01-01'::timestamp) as last_activity
                FROM reviewer_activities;

                CREATE UNIQUE INDEX IF NOT EXISTS idx_reviewer_stats_%s_reviewer 
                ON reviewer_stats_%s (reviewer_id);

                CREATE INDEX IF NOT EXISTS idx_reviewer_stats_%s_activity
                ON reviewer_stats_%s (last_activity DESC, reviewer_id);
            `, period.name,
				enum.ActivityTypeUserViewed,
				enum.ActivityTypeUserConfirmed,
				enum.ActivityTypeUserCleared,
				period.interval,
				enum.ActivityTypeUserViewed,
				enum.ActivityTypeUserConfirmed,
				enum.ActivityTypeUserCleared,
				period.name, period.name,
				period.name, period.name)

			_, err := db.NewRaw(query).Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to create materialized view for %s period: %w", period.name, err)
			}
		}

		return nil
	}, func(ctx context.Context, db *bun.DB) error {
		// Drop materialized views and indexes
		var dropViews strings.Builder

		for _, period := range periods {
			dropViews.WriteString(fmt.Sprintf(`
				DROP INDEX IF EXISTS idx_reviewer_stats_%s_reviewer;
				DROP INDEX IF EXISTS idx_reviewer_stats_%s_activity;
				DROP MATERIALIZED VIEW IF EXISTS reviewer_stats_%s;
			`, period.name, period.name, period.name))
		}

		_, err := db.NewRaw(dropViews.String()).Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to drop materialized views and indexes: %w", err)
		}

		return nil
	})
}

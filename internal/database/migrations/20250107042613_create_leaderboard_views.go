package migrations

import (
	"context"
	"fmt"
	"strings"

	"github.com/robalyx/rotector/internal/database/types/enum"
	"github.com/uptrace/bun"
)

func init() {
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
		// Create materialized views and indexes for leaderboard stats
		for _, period := range periods {
			var whereClause string
			if period.interval == "NULL" {
				whereClause = "TRUE" // all_time includes all records
			} else {
				whereClause = "voted_at > NOW() - " + period.interval
			}

			query := fmt.Sprintf(`
                CREATE MATERIALIZED VIEW IF NOT EXISTS vote_leaderboard_stats_%s AS
                SELECT 
                    discord_user_id,
                    COUNT(*) FILTER (WHERE is_correct = true) as correct_votes,
                    COUNT(*) as total_votes,
                    COALESCE(COUNT(*) FILTER (WHERE is_correct = true)::float / NULLIF(COUNT(*), 0), 0) as accuracy,
                    MAX(voted_at) as voted_at
                FROM vote_stats
                WHERE %s
                GROUP BY discord_user_id;

				CREATE UNIQUE INDEX IF NOT EXISTS idx_vote_leaderboard_%s_user 
				ON vote_leaderboard_stats_%s (discord_user_id);

				CREATE INDEX IF NOT EXISTS idx_vote_leaderboard_%s_sort 
				ON vote_leaderboard_stats_%s (correct_votes DESC, accuracy DESC, voted_at DESC, discord_user_id);
            `, period.name, whereClause, period.name, period.name, period.name, period.name)

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
				DROP INDEX IF EXISTS idx_vote_leaderboard_%s_user;
				DROP INDEX IF EXISTS idx_vote_leaderboard_%s_sort;
				DROP MATERIALIZED VIEW IF EXISTS vote_leaderboard_stats_%s;
			`, period.name, period.name, period.name))
		}

		_, err := db.NewRaw(dropViews.String()).Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to drop materialized views and indexes: %w", err)
		}

		return nil
	})
}

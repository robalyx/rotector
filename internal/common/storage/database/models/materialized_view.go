package models

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
	"github.com/uptrace/bun"
	"go.uber.org/zap"
)

// MaterializedViewModel handles refresh tracking for materialized views.
type MaterializedViewModel struct {
	db     *bun.DB
	logger *zap.Logger
}

// NewMaterializedView creates a new MaterializedViewModel.
func NewMaterializedView(db *bun.DB, logger *zap.Logger) *MaterializedViewModel {
	return &MaterializedViewModel{
		db:     db,
		logger: logger.Named("db_materialized_view"),
	}
}

// RefreshIfStale refreshes a materialized view if it hasn't been refreshed in the given duration.
func (m *MaterializedViewModel) RefreshIfStale(
	ctx context.Context, viewName string, staleDuration time.Duration,
) error {
	return m.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Get last refresh time
		var refresh types.MaterializedViewRefresh
		err := tx.NewSelect().
			Model(&refresh).
			Where("view_name = ?", viewName).
			For("UPDATE SKIP LOCKED").
			Scan(ctx)

		if err != nil || time.Since(refresh.LastRefresh) > staleDuration {
			// Refresh the materialized view
			_, err = tx.NewRaw(fmt.Sprintf(`
				REFRESH MATERIALIZED VIEW CONCURRENTLY %s
			`, viewName)).Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to refresh view %s: %w", viewName, err)
			}

			// Update last refresh time
			_, err = tx.NewInsert().
				Model(&types.MaterializedViewRefresh{
					ViewName:    viewName,
					LastRefresh: time.Now(),
				}).
				On("CONFLICT (view_name) DO UPDATE").
				Set("last_refresh = EXCLUDED.last_refresh").
				Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to update refresh time: %w", err)
			}
		}

		return nil
	})
}

// GetRefreshInfo returns the last refresh time and next scheduled refresh for a view.
func (m *MaterializedViewModel) GetRefreshInfo(
	ctx context.Context, viewName string,
) (lastRefresh time.Time, err error) {
	var refresh types.MaterializedViewRefresh
	err = m.db.NewSelect().
		Model(&refresh).
		Where("view_name = ?", viewName).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return time.Time{}, nil
		}
		return time.Time{}, fmt.Errorf("failed to get refresh info: %w", err)
	}

	return refresh.LastRefresh, nil
}

// RefreshLeaderboardView is a helper method for refreshing leaderboard views.
func (m *MaterializedViewModel) RefreshLeaderboardView(ctx context.Context, period enum.LeaderboardPeriod) error {
	viewName := fmt.Sprintf("vote_leaderboard_stats_%s", period)
	staleDuration := getLeaderboardStaleDuration(period)
	return m.RefreshIfStale(ctx, viewName, staleDuration)
}

// GetLeaderboardRefreshInfo returns the last refresh time and next scheduled refresh for a leaderboard view.
func (m *MaterializedViewModel) GetLeaderboardRefreshInfo(
	ctx context.Context, period enum.LeaderboardPeriod,
) (lastRefresh, nextRefresh time.Time, err error) {
	viewName := fmt.Sprintf("vote_leaderboard_stats_%s", period)
	staleDuration := getLeaderboardStaleDuration(period)

	lastRefresh, err = m.GetRefreshInfo(ctx, viewName)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}

	return lastRefresh, lastRefresh.Add(staleDuration), nil
}

// getLeaderboardStaleDuration returns the recommended refresh interval for a leaderboard period.
func getLeaderboardStaleDuration(period enum.LeaderboardPeriod) time.Duration {
	switch period {
	case enum.LeaderboardPeriodDaily:
		return 5 * time.Minute
	case enum.LeaderboardPeriodWeekly:
		return 15 * time.Minute
	case enum.LeaderboardPeriodBiWeekly:
		return 30 * time.Minute
	case enum.LeaderboardPeriodMonthly:
		return 1 * time.Hour
	case enum.LeaderboardPeriodBiAnnually:
		return 6 * time.Hour
	case enum.LeaderboardPeriodAnnually:
		return 12 * time.Hour
	case enum.LeaderboardPeriodAllTime:
		return 24 * time.Hour
	default:
		return 15 * time.Minute
	}
}

// RefreshReviewerStatsView refreshes reviewer statistics for a specific period.
func (m *MaterializedViewModel) RefreshReviewerStatsView(ctx context.Context, period enum.ReviewerStatsPeriod) error {
	viewName := fmt.Sprintf("reviewer_stats_%s", period)
	staleDuration := 30 * time.Minute
	return m.RefreshIfStale(ctx, viewName, staleDuration)
}

// GetReviewerStatsRefreshInfo returns the last refresh time and next scheduled refresh for a reviewer stats view.
func (m *MaterializedViewModel) GetReviewerStatsRefreshInfo(
	ctx context.Context, period enum.ReviewerStatsPeriod,
) (lastRefresh, nextRefresh time.Time, err error) {
	viewName := fmt.Sprintf("reviewer_stats_%s", period)
	staleDuration := 30 * time.Minute

	lastRefresh, err = m.GetRefreshInfo(ctx, viewName)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}

	return lastRefresh, lastRefresh.Add(staleDuration), nil
}

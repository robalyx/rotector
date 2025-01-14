package models

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/robalyx/rotector/internal/common/storage/database/types"
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
		logger: logger,
	}
}

// RefreshIfStale refreshes a materialized view if it hasn't been refreshed in the given duration.
func (m *MaterializedViewModel) RefreshIfStale(ctx context.Context, period types.LeaderboardPeriod) error {
	viewName := fmt.Sprintf("vote_leaderboard_stats_%s", period)
	staleDuration := m.getStaleDuration(period)

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
func (m *MaterializedViewModel) GetRefreshInfo(ctx context.Context, period types.LeaderboardPeriod) (lastRefresh, nextRefresh time.Time, err error) {
	viewName := fmt.Sprintf("vote_leaderboard_stats_%s", period)
	staleDuration := m.getStaleDuration(period)

	var refresh types.MaterializedViewRefresh
	err = m.db.NewSelect().
		Model(&refresh).
		Where("view_name = ?", viewName).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return time.Time{}, time.Time{}, nil
		}
		return time.Time{}, time.Time{}, fmt.Errorf("failed to get refresh info: %w", err)
	}

	return refresh.LastRefresh, refresh.LastRefresh.Add(staleDuration), nil
}

// getStaleDuration returns the recommended refresh interval for a period.
func (m *MaterializedViewModel) getStaleDuration(period types.LeaderboardPeriod) time.Duration {
	switch period {
	case types.LeaderboardPeriodDaily:
		return 5 * time.Minute
	case types.LeaderboardPeriodWeekly:
		return 15 * time.Minute
	case types.LeaderboardPeriodBiWeekly:
		return 30 * time.Minute
	case types.LeaderboardPeriodMonthly:
		return 1 * time.Hour
	case types.LeaderboardPeriodBiAnnually:
		return 6 * time.Hour
	case types.LeaderboardPeriodAnnually:
		return 12 * time.Hour
	case types.LeaderboardPeriodAllTime:
		return 24 * time.Hour
	default:
		return 15 * time.Minute
	}
}
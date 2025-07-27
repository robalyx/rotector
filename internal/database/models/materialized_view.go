package models

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/robalyx/rotector/internal/database/dbretry"
	"github.com/robalyx/rotector/internal/database/types"
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
	return dbretry.Transaction(ctx, m.db, func(ctx context.Context, tx bun.Tx) error {
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

// GetRefreshInfo returns the last refresh time for a view.
func (m *MaterializedViewModel) GetRefreshInfo(
	ctx context.Context, viewName string,
) (lastRefresh time.Time, err error) {
	return dbretry.Operation(ctx, func(ctx context.Context) (time.Time, error) {
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
	})
}

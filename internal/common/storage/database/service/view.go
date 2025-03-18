package service

import (
	"context"
	"fmt"
	"time"

	"github.com/robalyx/rotector/internal/common/storage/database/models"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
	"go.uber.org/zap"
)

// ViewService handles materialized view business logic.
type ViewService struct {
	model  *models.MaterializedViewModel
	logger *zap.Logger
}

// NewView creates a new view service.
func NewView(model *models.MaterializedViewModel, logger *zap.Logger) *ViewService {
	return &ViewService{
		model:  model,
		logger: logger.Named("view_service"),
	}
}

// RefreshLeaderboardView refreshes a leaderboard view if it has become stale.
func (s *ViewService) RefreshLeaderboardView(ctx context.Context, period enum.LeaderboardPeriod) error {
	viewName := fmt.Sprintf("vote_leaderboard_stats_%s", period)
	staleDuration := s.getLeaderboardStaleDuration(period)
	return s.model.RefreshIfStale(ctx, viewName, staleDuration)
}

// GetLeaderboardRefreshInfo returns the last refresh time and next scheduled refresh for a leaderboard view.
func (s *ViewService) GetLeaderboardRefreshInfo(
	ctx context.Context, period enum.LeaderboardPeriod,
) (lastRefresh, nextRefresh time.Time, err error) {
	viewName := fmt.Sprintf("vote_leaderboard_stats_%s", period)
	staleDuration := s.getLeaderboardStaleDuration(period)

	lastRefresh, err = s.model.GetRefreshInfo(ctx, viewName)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}

	return lastRefresh, lastRefresh.Add(staleDuration), nil
}

// getLeaderboardStaleDuration returns the recommended refresh interval for a leaderboard period.
func (s *ViewService) getLeaderboardStaleDuration(period enum.LeaderboardPeriod) time.Duration {
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
func (s *ViewService) RefreshReviewerStatsView(ctx context.Context, period enum.ReviewerStatsPeriod) error {
	viewName := fmt.Sprintf("reviewer_stats_%s", period)
	staleDuration := 30 * time.Minute
	return s.model.RefreshIfStale(ctx, viewName, staleDuration)
}

// GetReviewerStatsRefreshInfo returns the last refresh time and next scheduled refresh for a reviewer stats view.
func (s *ViewService) GetReviewerStatsRefreshInfo(
	ctx context.Context, period enum.ReviewerStatsPeriod,
) (lastRefresh, nextRefresh time.Time, err error) {
	viewName := fmt.Sprintf("reviewer_stats_%s", period)
	staleDuration := 30 * time.Minute

	lastRefresh, err = s.model.GetRefreshInfo(ctx, viewName)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}

	return lastRefresh, lastRefresh.Add(staleDuration), nil
}

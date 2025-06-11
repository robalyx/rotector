package service

import (
	"context"
	"fmt"
	"time"

	"github.com/robalyx/rotector/internal/database/models"
	"github.com/robalyx/rotector/internal/database/types/enum"
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

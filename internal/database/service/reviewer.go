package service

import (
	"context"

	"github.com/robalyx/rotector/internal/database/models"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/database/types/enum"
	"go.uber.org/zap"
)

// ReviewerService handles reviewer-related business logic.
type ReviewerService struct {
	model  *models.ReviewerModel
	views  *ViewService
	logger *zap.Logger
}

// NewReviewer creates a new reviewer service.
func NewReviewer(
	model *models.ReviewerModel,
	views *ViewService,
	logger *zap.Logger,
) *ReviewerService {
	return &ReviewerService{
		model:  model,
		views:  views,
		logger: logger.Named("reviewer_service"),
	}
}

// GetReviewerStats retrieves paginated reviewer statistics for a specific time period.
func (s *ReviewerService) GetReviewerStats(
	ctx context.Context,
	period enum.ReviewerStatsPeriod,
	cursor *types.ReviewerStatsCursor,
	limit int,
) (map[uint64]*types.ReviewerStats, *types.ReviewerStatsCursor, error) {
	// Try to refresh the materialized view
	err := s.views.RefreshReviewerStatsView(ctx, period)
	if err != nil {
		s.logger.Warn("Failed to refresh reviewer stats view",
			zap.Error(err),
			zap.String("period", period.String()))
		// Continue anyway - we'll use slightly stale data
	}

	// Get stats from the model
	return s.model.GetReviewerStats(ctx, period, cursor, limit)
}

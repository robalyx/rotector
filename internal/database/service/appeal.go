package service

import (
	"context"
	"fmt"

	"github.com/robalyx/rotector/internal/database/models"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/database/types/enum"
	"go.uber.org/zap"
)

// AppealService handles appeal-related business logic.
type AppealService struct {
	model  *models.AppealModel
	logger *zap.Logger
}

// NewAppeal creates a new appeal service.
func NewAppeal(model *models.AppealModel, logger *zap.Logger) *AppealService {
	return &AppealService{
		model:  model,
		logger: logger.Named("appeal_service"),
	}
}

// GetAppealsToReview gets a list of appeals based on sort criteria.
// It wraps the model-level function and handles pagination processing.
func (s *AppealService) GetAppealsToReview(
	ctx context.Context,
	sortBy enum.AppealSortBy,
	statusFilter enum.AppealStatus,
	reviewerID uint64,
	cursor *types.AppealTimeline,
	limit int,
) ([]*types.FullAppeal, *types.AppealTimeline, *types.AppealTimeline, error) {
	// Get appeals from the model layer
	results, err := s.model.GetAppealsToReview(ctx, sortBy, statusFilter, reviewerID, cursor, limit)
	if err != nil {
		return nil, nil, nil, fmt.Errorf(
			"failed to get appeals with cursor: %w (sortBy=%s, reviewerID=%d)",
			err, sortBy.String(), reviewerID,
		)
	}

	// Process results to get cursors for pagination
	results, firstCursor, nextCursor := s.processAppealResults(results, limit)
	return results, firstCursor, nextCursor, nil
}

// GetAppealsByRequester gets all appeals submitted by a specific user.
// It wraps the model-level function and handles pagination processing.
func (s *AppealService) GetAppealsByRequester(
	ctx context.Context,
	statusFilter enum.AppealStatus,
	requesterID uint64,
	cursor *types.AppealTimeline,
	limit int,
) ([]*types.FullAppeal, *types.AppealTimeline, *types.AppealTimeline, error) {
	// Get appeals from the model layer
	results, err := s.model.GetAppealsByRequester(ctx, statusFilter, requesterID, cursor, limit)
	if err != nil {
		return nil, nil, nil, fmt.Errorf(
			"failed to get appeals by requester: %w (requesterID=%d)",
			err, requesterID,
		)
	}

	// Process results to get cursors for pagination
	results, firstCursor, nextCursor := s.processAppealResults(results, limit)
	return results, firstCursor, nextCursor, nil
}

// processAppealResults handles pagination and data transformation for appeal results.
func (s *AppealService) processAppealResults(
	results []*types.FullAppeal, limit int,
) ([]*types.FullAppeal, *types.AppealTimeline, *types.AppealTimeline) {
	var nextCursor *types.AppealTimeline
	var firstCursor *types.AppealTimeline

	if len(results) > limit {
		// Use the extra item as the next cursor for pagination
		extraItem := results[limit]
		nextCursor = &types.AppealTimeline{
			ID:           extraItem.ID,
			Timestamp:    extraItem.Timestamp,
			LastViewed:   extraItem.LastViewed,
			LastActivity: extraItem.LastActivity,
		}
		results = results[:limit] // Remove the extra item from results
	}

	if len(results) > 0 {
		// Create first page cursor for navigation back to start
		first := results[0]
		firstCursor = &types.AppealTimeline{
			ID:           first.ID,
			Timestamp:    first.Timestamp,
			LastViewed:   first.LastViewed,
			LastActivity: first.LastActivity,
		}
	}

	return results, firstCursor, nextCursor
}

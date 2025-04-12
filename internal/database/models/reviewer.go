package models

import (
	"context"
	"fmt"
	"time"

	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/database/types/enum"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"go.uber.org/zap"
)

// ReviewerModel handles database operations for reviewer statistics.
type ReviewerModel struct {
	db     *bun.DB
	logger *zap.Logger
}

// NewReviewer creates a new ReviewerModel instance.
func NewReviewer(db *bun.DB, logger *zap.Logger) *ReviewerModel {
	return &ReviewerModel{
		db:     db,
		logger: logger.Named("db_reviewer"),
	}
}

// GetReviewerStats retrieves paginated reviewer statistics for a specific time period.
//
// Deprecated: Use Service().Reviewer().GetReviewerStats() instead.
func (r *ReviewerModel) GetReviewerStats(
	ctx context.Context, period enum.ReviewerStatsPeriod, cursor *types.ReviewerStatsCursor, limit int,
) (map[uint64]*types.ReviewerStats, *types.ReviewerStatsCursor, error) {
	// Initialize result map
	results := make(map[uint64]*types.ReviewerStats)
	var nextCursor *types.ReviewerStatsCursor

	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Get bot settings to filter for valid reviewer IDs
		var settings types.BotSetting
		err := tx.NewSelect().
			Model(&settings).
			Where("id = 1").
			Scan(ctx)
		if err != nil {
			return fmt.Errorf("failed to get bot settings: %w", err)
		}

		// Return empty results if no reviewers configured
		if len(settings.ReviewerIDs) == 0 {
			return nil
		}

		query := tx.NewSelect().
			TableExpr("reviewer_stats_"+period.String()).
			Where("reviewer_id = ANY(?)", pgdialect.Array(settings.ReviewerIDs)).
			Order("last_activity DESC", "reviewer_id").
			Limit(limit + 1)

		// Add cursor condition if provided
		if cursor != nil {
			query = query.Where("(last_activity, reviewer_id) < (?, ?)",
				cursor.LastActivity, cursor.ReviewerID)
		}

		var stats []*types.ReviewerStats
		err = query.Scan(ctx, &stats)
		if err != nil {
			return fmt.Errorf("failed to scan reviewer stats: %w", err)
		}

		// Check if there are more results
		if len(stats) > limit {
			last := stats[limit-1]
			nextCursor = &types.ReviewerStatsCursor{
				LastActivity: last.LastActivity,
				ReviewerID:   last.ReviewerID,
			}
			stats = stats[:limit] // Remove the extra item
		}

		// Store stats in results map
		for _, stat := range stats {
			results[stat.ReviewerID] = stat
		}

		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	return results, nextCursor, nil
}

// GetReviewerInfos gets reviewer info records for the specified reviewer IDs.
func (r *ReviewerModel) GetReviewerInfos(ctx context.Context, reviewerIDs []uint64) (map[uint64]*types.ReviewerInfo, error) {
	var reviewers []*types.ReviewerInfo

	err := r.db.NewSelect().
		Model(&reviewers).
		Where("user_id IN (?)", bun.In(reviewerIDs)).
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get reviewer infos: %w", err)
	}

	// Convert to map for easier lookup
	result := make(map[uint64]*types.ReviewerInfo, len(reviewers))
	for _, reviewer := range reviewers {
		result[reviewer.UserID] = reviewer
	}

	return result, nil
}

// GetReviewerInfosForUpdate gets all reviewer info records that need updating.
func (r *ReviewerModel) GetReviewerInfosForUpdate(
	ctx context.Context, maxAge time.Duration,
) (map[uint64]*types.ReviewerInfo, error) {
	var reviewers []*types.ReviewerInfo
	cutoff := time.Now().Add(-maxAge)

	err := r.db.NewSelect().
		Model(&reviewers).
		Where("updated_at < ?", cutoff).
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get reviewer infos: %w", err)
	}

	// Convert to map for easier lookup
	result := make(map[uint64]*types.ReviewerInfo, len(reviewers))
	for _, reviewer := range reviewers {
		result[reviewer.UserID] = reviewer
	}

	return result, nil
}

// SaveReviewerInfos saves or updates reviewer info records.
func (r *ReviewerModel) SaveReviewerInfos(ctx context.Context, reviewers []*types.ReviewerInfo) error {
	_, err := r.db.NewInsert().
		Model(&reviewers).
		On("CONFLICT (user_id) DO UPDATE").
		Set("username = EXCLUDED.username").
		Set("display_name = EXCLUDED.display_name").
		Set("updated_at = EXCLUDED.updated_at").
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to save reviewer infos: %w", err)
	}

	return nil
}

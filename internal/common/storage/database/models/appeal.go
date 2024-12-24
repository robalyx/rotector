package models

import (
	"context"
	"fmt"
	"time"

	"github.com/rotector/rotector/internal/common/storage/database/types"
	"github.com/uptrace/bun"
	"go.uber.org/zap"
)

// appealResult is a struct that holds an appeal and its timeline.
type appealResult struct {
	*types.Appeal `bun:"embed:"`
	Timeline      struct {
		Timestamp    time.Time `bun:",pk,notnull"`
		LastViewed   time.Time `bun:",notnull"`
		LastActivity time.Time `bun:",notnull"`
	} `bun:"embed:"`
}

// AppealModel handles database operations for appeal records.
type AppealModel struct {
	db     *bun.DB
	logger *zap.Logger
}

// NewAppeal creates an AppealModel with database access.
func NewAppeal(db *bun.DB, logger *zap.Logger) *AppealModel {
	return &AppealModel{
		db:     db,
		logger: logger,
	}
}

// CreateAppeal submits a new appeal request.
func (r *AppealModel) CreateAppeal(ctx context.Context, appeal *types.Appeal, reason string) error {
	now := time.Now()
	appeal.Status = types.AppealStatusPending

	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Create the appeal
		_, err := tx.NewInsert().Model(appeal).Exec(ctx)
		if err != nil {
			return fmt.Errorf(
				"failed to create appeal: %w (userID=%d, requesterID=%d)",
				err, appeal.UserID, appeal.RequesterID,
			)
		}

		// Create timeline entry
		timeline := &types.AppealTimeline{
			ID:           appeal.ID,
			Timestamp:    now,
			LastViewed:   now,
			LastActivity: now,
		}
		_, err = tx.NewInsert().Model(timeline).Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to create appeal timeline: %w (appealID=%d)", err, appeal.ID)
		}

		// Create initial message
		message := &types.AppealMessage{
			AppealID:  appeal.ID,
			UserID:    appeal.RequesterID,
			Role:      types.MessageRoleUser,
			Content:   reason,
			CreatedAt: now,
		}
		_, err = tx.NewInsert().Model(message).Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to create initial appeal message: %w (appealID=%d)", err, appeal.ID)
		}

		r.logger.Debug("Created appeal",
			zap.Int64("id", appeal.ID),
			zap.Uint64("userID", appeal.UserID),
			zap.Uint64("requesterID", appeal.RequesterID))
		return nil
	})
}

// AcceptAppeal marks an appeal as accepted and updates its status.
func (r *AppealModel) AcceptAppeal(ctx context.Context, appealID int64, reviewerID uint64, reason string) error {
	now := time.Now()
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Update appeal status
		_, err := tx.NewUpdate().
			Model((*types.Appeal)(nil)).
			Set("status = ?", types.AppealStatusAccepted).
			Set("reviewer_id = ?", reviewerID).
			Set("reviewed_at = ?", now).
			Set("review_reason = ?", reason).
			Where("id = ?", appealID).
			Where("status = ?", types.AppealStatusPending).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to accept appeal: %w (appealID=%d)", err, appealID)
		}

		// Update timeline
		_, err = tx.NewUpdate().
			Model((*types.AppealTimeline)(nil)).
			Set("last_activity = ?", now).
			Where("id = ?", appealID).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to update appeal timeline: %w (appealID=%d)", err, appealID)
		}

		r.logger.Debug("Accepted appeal",
			zap.Int64("appealID", appealID),
			zap.Uint64("reviewerID", reviewerID))
		return nil
	})
}

// RejectAppeal marks an appeal as rejected and updates its status.
func (r *AppealModel) RejectAppeal(ctx context.Context, appealID int64, reviewerID uint64, reason string) error {
	now := time.Now()
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Update appeal status
		_, err := tx.NewUpdate().
			Model((*types.Appeal)(nil)).
			Set("status = ?", types.AppealStatusRejected).
			Set("reviewer_id = ?", reviewerID).
			Set("reviewed_at = ?", now).
			Set("review_reason = ?", reason).
			Where("id = ?", appealID).
			Where("status = ?", types.AppealStatusPending).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to reject appeal: %w (appealID=%d)", err, appealID)
		}

		// Update timeline
		_, err = tx.NewUpdate().
			Model((*types.AppealTimeline)(nil)).
			Set("last_activity = ?", now).
			Where("id = ?", appealID).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to update appeal timeline: %w (appealID=%d)", err, appealID)
		}

		r.logger.Debug("Rejected appeal",
			zap.Int64("appealID", appealID),
			zap.Uint64("reviewerID", reviewerID))
		return nil
	})
}

// HasPendingAppealByRequester checks if a requester already has any pending appeals.
func (r *AppealModel) HasPendingAppealByRequester(ctx context.Context, requesterID uint64) (bool, error) {
	exists, err := r.db.NewSelect().
		Model((*types.Appeal)(nil)).
		Where("requester_id = ?", requesterID).
		Where("status = ?", types.AppealStatusPending).
		Exists(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to check pending appeals: %w (requesterID=%d)", err, requesterID)
	}
	return exists, nil
}

// HasPreviousRejection checks if a user ID has any rejected appeals.
func (r *AppealModel) HasPreviousRejection(ctx context.Context, userID uint64) (bool, error) {
	exists, err := r.db.NewSelect().
		Model((*types.Appeal)(nil)).
		Where("user_id = ?", userID).
		Where("status = ?", types.AppealStatusRejected).
		Exists(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to check previous rejections: %w (userID=%d)", err, userID)
	}

	return exists, nil
}

// GetAppealsToReview gets a list of appeals based on sort criteria.
// It supports pagination through cursors and different sorting options.
func (r *AppealModel) GetAppealsToReview(
	ctx context.Context,
	sortBy types.AppealSortBy,
	statusFilter types.AppealFilterBy,
	reviewerID uint64,
	cursor *types.AppealTimeline,
	limit int,
) ([]*types.Appeal, *types.AppealTimeline, *types.AppealTimeline, error) {
	// Build base query with timeline join
	query := r.db.NewSelect().
		Model((*types.Appeal)(nil)).
		Join("JOIN appeal_timelines AS t ON t.id = appeal.id").
		ColumnExpr("appeal.*").
		ColumnExpr("t.timestamp, t.last_viewed, t.last_activity")

	// Apply status filter if not showing all
	if statusFilter != types.AppealFilterByAll {
		query.Where("status = ?", statusFilter)
	}

	// Apply sort order and cursor conditions based on sort type
	switch sortBy {
	case types.AppealSortByOldest:
		if cursor != nil {
			query.Where("(t.timestamp, appeal.id) >= (?, ?)", cursor.Timestamp, cursor.ID)
		}
		query.Order("t.timestamp ASC", "appeal.id ASC")
	case types.AppealSortByClaimed:
		query.Where("status = ?", types.AppealStatusPending) // Only show pending appeals for claimed view
		query.Where("claimed_by = ?", reviewerID)
		if cursor != nil {
			query.Where("(t.last_activity, appeal.id) <= (?, ?)", cursor.LastActivity, cursor.ID)
		}
		query.Order("t.last_activity DESC", "appeal.id DESC")
	case types.AppealSortByNewest:
		if cursor != nil {
			query.Where("(t.timestamp, appeal.id) <= (?, ?)", cursor.Timestamp, cursor.ID)
		}
		query.Order("t.timestamp DESC", "appeal.id DESC")
	}

	// Get one extra to determine if there are more results
	query.Limit(limit + 1)

	var results []appealResult
	err := query.Scan(ctx, &results)
	if err != nil {
		return nil, nil, nil, fmt.Errorf(
			"failed to get appeals with cursor: %w (sortBy=%s, reviewerID=%d)",
			err, string(sortBy), reviewerID,
		)
	}

	// Process results to get appeals and cursors for pagination
	appeals, firstCursor, nextCursor := processAppealResults(results, limit)
	return appeals, firstCursor, nextCursor, nil
}

// GetAppealsByRequester gets all appeals submitted by a specific user.
func (r *AppealModel) GetAppealsByRequester(
	ctx context.Context,
	statusFilter types.AppealFilterBy,
	requesterID uint64,
	cursor *types.AppealTimeline,
	limit int,
) ([]*types.Appeal, *types.AppealTimeline, *types.AppealTimeline, error) {
	query := r.db.NewSelect().
		Model((*types.Appeal)(nil)).
		Join("JOIN appeal_timelines AS t ON t.id = appeal.id").
		ColumnExpr("appeal.*").
		ColumnExpr("t.timestamp, t.last_viewed, t.last_activity").
		Where("requester_id = ?", requesterID)

	// Apply status filter if not showing all
	if statusFilter != types.AppealFilterByAll {
		query.Where("status = ?", statusFilter)
	}

	// Apply cursor conditions if cursor exists
	if cursor != nil {
		query = query.Where("(t.timestamp, appeal.id) <= (?, ?)", cursor.Timestamp, cursor.ID)
	}

	query.Order("t.timestamp DESC", "appeal.id DESC").
		Limit(limit + 1) // Get one extra to determine if there are more results

	var results []appealResult

	err := query.Scan(ctx, &results)
	if err != nil {
		return nil, nil, nil, fmt.Errorf(
			"failed to get appeals by requester: %w (requesterID=%d)",
			err, requesterID,
		)
	}

	// Process results to get appeals and cursors for pagination
	appeals, firstCursor, nextCursor := processAppealResults(results, limit)
	return appeals, firstCursor, nextCursor, nil
}

// GetAppealMessages gets the messages for an appeal.
func (r *AppealModel) GetAppealMessages(ctx context.Context, appealID int64) ([]*types.AppealMessage, error) {
	var messages []*types.AppealMessage
	err := r.db.NewSelect().
		Model(&messages).
		Where("appeal_id = ?", appealID).
		Order("created_at ASC").
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get appeal messages: %w (appealID=%d)", err, appealID)
	}

	return messages, nil
}

// AddAppealMessage adds a new message to an appeal and updates the appeal's last activity.
// If the message is from a moderator and the appeal isn't claimed, it will also claim the appeal.
func (r *AppealModel) AddAppealMessage(ctx context.Context, message *types.AppealMessage, appeal *types.Appeal) error {
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Insert the new message
		if _, err := tx.NewInsert().Model(message).Exec(ctx); err != nil {
			return fmt.Errorf("failed to insert appeal message: %w (appealID=%d)", err, appeal.ID)
		}

		now := time.Now()

		// Auto-claim appeal for moderator messages if not already claimed
		if message.Role == types.MessageRoleModerator && appeal.ClaimedBy == 0 {
			_, err := tx.NewUpdate().
				Model(appeal).
				Set("claimed_by = ?", message.UserID).
				Set("claimed_at = ?", now).
				Where("id = ?", appeal.ID).
				Exec(ctx)
			if err != nil {
				return fmt.Errorf("failed to update appeal: %w (appealID=%d)", err, appeal.ID)
			}
		}

		// Update the appeal's last activity timestamp
		_, err := tx.NewUpdate().
			Model((*types.AppealTimeline)(nil)).
			Set("last_activity = ?", now).
			Where("id = ?", appeal.ID).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to update appeal timeline: %w (appealID=%d)", err, appeal.ID)
		}

		return nil
	})
}

// processAppealResults handles pagination and data transformation for appeal results.
func processAppealResults(results []appealResult, limit int) ([]*types.Appeal, *types.AppealTimeline, *types.AppealTimeline) {
	var appeals []*types.Appeal
	var nextCursor *types.AppealTimeline
	var firstCursor *types.AppealTimeline

	if len(results) > limit {
		// Use the extra item as the next cursor for pagination
		last := results[limit]
		nextCursor = &types.AppealTimeline{
			ID:           last.Appeal.ID,
			Timestamp:    last.Timeline.Timestamp,
			LastViewed:   last.Timeline.LastViewed,
			LastActivity: last.Timeline.LastActivity,
		}
		results = results[:limit] // Remove the extra item from results
	}

	// Transform appeal results into appeal objects with timeline data
	appeals = make([]*types.Appeal, len(results))
	for i, result := range results {
		appeals[i] = result.Appeal
		appeals[i].Timestamp = result.Timeline.Timestamp
		appeals[i].LastViewed = result.Timeline.LastViewed
		appeals[i].LastActivity = result.Timeline.LastActivity
	}

	if len(results) > 0 {
		// Create first page cursor for navigation back to start
		first := results[0]
		firstCursor = &types.AppealTimeline{
			ID:           first.Appeal.ID,
			Timestamp:    first.Timeline.Timestamp,
			LastViewed:   first.Timeline.LastViewed,
			LastActivity: first.Timeline.LastActivity,
		}
	}

	return appeals, firstCursor, nextCursor
}

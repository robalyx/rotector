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

// AppealModel handles database operations for appeal records.
type AppealModel struct {
	db     *bun.DB
	logger *zap.Logger
}

// NewAppeal creates an AppealModel with database access.
func NewAppeal(db *bun.DB, logger *zap.Logger) *AppealModel {
	return &AppealModel{
		db:     db,
		logger: logger.Named("db_appeal"),
	}
}

// CreateAppeal submits a new appeal request.
func (r *AppealModel) CreateAppeal(ctx context.Context, appeal *types.Appeal, reason string) error {
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		now := time.Now()

		// Set creation timestamp
		appeal.Timestamp = now

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
			Role:      enum.MessageRoleUser,
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
			zap.Uint64("requesterID", appeal.RequesterID),
			zap.String("status", appeal.Status.String()))

		return nil
	})
}

// AcceptAppeal marks an appeal as accepted and updates its status.
func (r *AppealModel) AcceptAppeal(ctx context.Context, appealID int64, timestamp time.Time, reason string) error {
	now := time.Now()
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Update appeal status
		_, err := tx.NewUpdate().
			Model((*types.Appeal)(nil)).
			Set("status = ?", enum.AppealStatusAccepted).
			Set("review_reason = ?", reason).
			Where("id = ?", appealID).
			Where("status = ?", enum.AppealStatusPending).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to accept appeal: %w (appealID=%d)", err, appealID)
		}

		// Update timeline
		_, err = tx.NewUpdate().
			Model((*types.AppealTimeline)(nil)).
			Set("last_activity = ?", now).
			Where("id = ?", appealID).
			Where("timestamp = ?", timestamp).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to update appeal timeline: %w (appealID=%d)", err, appealID)
		}

		r.logger.Debug("Accepted appeal",
			zap.Int64("appealID", appealID))
		return nil
	})
}

// RejectAppeal marks an appeal as rejected and updates its status.
func (r *AppealModel) RejectAppeal(ctx context.Context, appealID int64, timestamp time.Time, reason string) error {
	now := time.Now()
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Update appeal status
		_, err := tx.NewUpdate().
			Model((*types.Appeal)(nil)).
			Set("status = ?", enum.AppealStatusRejected).
			Set("review_reason = ?", reason).
			Where("id = ?", appealID).
			Where("status = ?", enum.AppealStatusPending).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to reject appeal: %w (appealID=%d)", err, appealID)
		}

		// Update timeline
		_, err = tx.NewUpdate().
			Model((*types.AppealTimeline)(nil)).
			Set("last_activity = ?", now).
			Where("id = ?", appealID).
			Where("timestamp = ?", timestamp).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to update appeal timeline: %w (appealID=%d)", err, appealID)
		}

		r.logger.Debug("Rejected appeal",
			zap.Int64("appealID", appealID))
		return nil
	})
}

// HasPendingAppealByRequester checks if a requester already has any pending appeals.
func (r *AppealModel) HasPendingAppealByRequester(
	ctx context.Context, requesterID uint64, appealType enum.AppealType,
) (bool, error) {
	exists, err := r.db.NewSelect().
		Model((*types.Appeal)(nil)).
		Where("requester_id = ?", requesterID).
		Where("status = ?", enum.AppealStatusPending).
		Where("type = ?", appealType).
		Exists(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to check pending appeals: %w (requesterID=%d)", err, requesterID)
	}
	return exists, nil
}

// GetAppealByID retrieves an appeal by its ID with fresh database state.
func (r *AppealModel) GetAppealByID(ctx context.Context, appealID int64) (*types.FullAppeal, error) {
	var fullAppeal types.FullAppeal
	fullAppeal.Appeal = new(types.Appeal)

	// Query both appeal and its timeline
	err := r.db.NewSelect().
		Model((*types.Appeal)(nil)).
		Column("appeal.*").
		ColumnExpr("t.last_viewed, t.last_activity").
		Join("JOIN appeal_timelines AS t ON t.id = appeal.id AND t.timestamp = appeal.timestamp").
		Where("appeal.id = ?", appealID).
		Scan(ctx, &fullAppeal)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, types.ErrNoAppealsFound
		}
		r.logger.Error("Failed to get appeal", zap.Error(err))
		return nil, fmt.Errorf("failed to get appeal: %w", err)
	}
	return &fullAppeal, nil
}

// HasPreviousRejection checks if a user ID has any rejected appeals within the last 3 days.
func (r *AppealModel) HasPreviousRejection(ctx context.Context, userID uint64, appealType enum.AppealType) (bool, error) {
	exists, err := r.db.NewSelect().
		Model((*types.Appeal)(nil)).
		Where("user_id = ?", userID).
		Where("status = ?", enum.AppealStatusRejected).
		Where("type = ?", appealType).
		Where("claimed_at > ?", time.Now().AddDate(0, 0, -3)).
		Exists(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to check previous rejections: %w (userID=%d)", err, userID)
	}

	return exists, nil
}

// HasPendingAppealByUserID checks if a user ID already has any pending appeals.
func (r *AppealModel) HasPendingAppealByUserID(
	ctx context.Context, userID uint64, appealType enum.AppealType,
) (bool, error) {
	exists, err := r.db.NewSelect().
		Model((*types.Appeal)(nil)).
		Where("user_id = ?", userID).
		Where("status = ?", enum.AppealStatusPending).
		Where("type = ?", appealType).
		Exists(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to check pending appeals: %w (userID=%d)", err, userID)
	}
	return exists, nil
}

// GetAppealsToReview gets a list of appeals based on sort criteria.
// It supports pagination through cursors and different sorting options.
//
// Deprecated: Use Service().Appeal().GetAppealsToReview() instead.
func (r *AppealModel) GetAppealsToReview(
	ctx context.Context,
	sortBy enum.AppealSortBy,
	statusFilter enum.AppealStatus,
	reviewerID uint64,
	cursor *types.AppealTimeline,
	limit int,
) ([]*types.FullAppeal, error) {
	// Build base query with timeline join
	query := r.db.NewSelect().
		Model((*types.Appeal)(nil)).
		Join("JOIN appeal_timelines AS t ON t.id = appeal.id AND t.timestamp = appeal.timestamp").
		ColumnExpr("appeal.*").
		ColumnExpr("t.last_viewed, t.last_activity")

	// Apply status filter if not showing all
	query.Where("status = ?", statusFilter)

	// Apply sort order and cursor conditions based on sort type
	switch sortBy {
	case enum.AppealSortByOldest:
		if cursor != nil {
			query.Where("(appeal.timestamp, appeal.id) > (?, ?)", cursor.Timestamp, cursor.ID)
		}
		query.Order("appeal.timestamp ASC", "appeal.id ASC")
	case enum.AppealSortByClaimed:
		query.Where("claimed_by = ?", reviewerID)
		if cursor != nil {
			query.Where("(t.last_activity, appeal.id) < (?, ?)", cursor.LastActivity, cursor.ID)
		}
		query.Order("t.last_activity DESC", "appeal.id DESC")
	case enum.AppealSortByNewest:
		if cursor != nil {
			query.Where("(appeal.timestamp, appeal.id) < (?, ?)", cursor.Timestamp, cursor.ID)
		}
		query.Order("appeal.timestamp DESC", "appeal.id DESC")
	}

	// Get one extra to determine if there are more results
	query.Limit(limit + 1)

	var results []*types.FullAppeal
	err := query.Scan(ctx, &results)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to get appeals with cursor: %w (sortBy=%s, reviewerID=%d)",
			err, sortBy.String(), reviewerID,
		)
	}

	return results, nil
}

// GetAppealsByRequester gets all appeals submitted by a specific user.
//
// Deprecated: Use Service().Appeal().GetAppealsByRequester() instead.
func (r *AppealModel) GetAppealsByRequester(
	ctx context.Context,
	statusFilter enum.AppealStatus,
	requesterID uint64,
	cursor *types.AppealTimeline,
	limit int,
) ([]*types.FullAppeal, error) {
	query := r.db.NewSelect().
		Model((*types.Appeal)(nil)).
		Join("JOIN appeal_timelines AS t ON t.id = appeal.id AND t.timestamp = appeal.timestamp").
		ColumnExpr("appeal.*").
		ColumnExpr("t.last_viewed, t.last_activity").
		Where("requester_id = ?", requesterID)

	// Apply status filter if not showing all
	query.Where("status = ?", statusFilter)

	// Apply cursor conditions if cursor exists
	if cursor != nil {
		query = query.Where("(appeal.timestamp, appeal.id) < (?, ?)", cursor.Timestamp, cursor.ID)
	}

	query.Order("appeal.timestamp DESC", "appeal.id DESC").
		Limit(limit + 1) // Get one extra to determine if there are more results

	var results []*types.FullAppeal
	err := query.Scan(ctx, &results)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to get appeals by requester: %w (requesterID=%d)",
			err, requesterID,
		)
	}

	return results, nil
}

// GetRejectedAppealsCount returns the total number of rejected appeals for a user ID.
func (r *AppealModel) GetRejectedAppealsCount(ctx context.Context, userID uint64) (int, error) {
	count, err := r.db.NewSelect().
		Model((*types.Appeal)(nil)).
		Where("user_id = ?", userID).
		Where("status = ?", enum.AppealStatusRejected).
		Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to count rejected appeals: %w (userID=%d)", err, userID)
	}
	return count, nil
}

// IsUserBlacklisted checks if a user ID is blacklisted from submitting appeals of a specific type.
func (r *AppealModel) IsUserBlacklisted(ctx context.Context, userID uint64, appealType enum.AppealType) (bool, error) {
	exists, err := r.db.NewSelect().
		Model((*types.AppealBlacklist)(nil)).
		Where("user_id = ?", userID).
		Where("type = ?", appealType).
		Exists(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to check appeal blacklist: %w (userID=%d, type=%s)", err, userID, appealType)
	}
	return exists, nil
}

// BlacklistUser adds a user ID to the appeal blacklist.
func (r *AppealModel) BlacklistUser(ctx context.Context, blacklist *types.AppealBlacklist) error {
	_, err := r.db.NewInsert().
		Model(blacklist).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to blacklist user: %w (userID=%d, type=%s)", err, blacklist.UserID, blacklist.Type)
	}

	r.logger.Debug("Added user to appeal blacklist",
		zap.Uint64("userID", blacklist.UserID),
		zap.String("type", blacklist.Type.String()),
		zap.Uint64("reviewerID", blacklist.ReviewerID),
		zap.Int64("appealID", blacklist.AppealID))
	return nil
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
func (r *AppealModel) AddAppealMessage(
	ctx context.Context, message *types.AppealMessage, appealID int64, timestamp time.Time,
) error {
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Insert the new message
		if _, err := tx.NewInsert().Model(message).Exec(ctx); err != nil {
			return fmt.Errorf("failed to insert appeal message: %w (appealID=%d)", err, appealID)
		}

		now := time.Now()

		// Update the appeal's last activity timestamp
		_, err := tx.NewUpdate().
			Model((*types.AppealTimeline)(nil)).
			Set("last_activity = ?", now).
			Where("id = ?", appealID).
			Where("timestamp = ?", timestamp).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to update appeal timeline: %w (appealID=%d)", err, appealID)
		}

		return nil
	})
}

// ClaimAppeal claims an appeal by setting the reviewer ID and timestamp.
func (r *AppealModel) ClaimAppeal(ctx context.Context, appealID int64, timestamp time.Time, reviewerID uint64) error {
	now := time.Now()
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Update the appeal to set claimed by and claimed at
		_, err := tx.NewUpdate().
			Model((*types.Appeal)(nil)).
			Set("claimed_by = ?", reviewerID).
			Set("claimed_at = ?", now).
			Where("id = ?", appealID).
			Where("status = ?", enum.AppealStatusPending).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to claim appeal: %w (appealID=%d)", err, appealID)
		}

		// Update the appeal timeline to set last viewed
		_, err = tx.NewUpdate().
			Model((*types.AppealTimeline)(nil)).
			Set("last_viewed = ?", now).
			Where("id = ?", appealID).
			Where("timestamp = ?", timestamp).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to update appeal timeline: %w (appealID=%d)", err, appealID)
		}

		return nil
	})
}

// ReopenAppeal changes a rejected or accepted appeal back to pending status and claims it for the reviewer.
func (r *AppealModel) ReopenAppeal(ctx context.Context, appealID int64, timestamp time.Time, reviewerID uint64) error {
	now := time.Now()
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Update appeal status
		_, err := tx.NewUpdate().
			Model((*types.Appeal)(nil)).
			Set("status = ?", enum.AppealStatusPending).
			Set("claimed_by = ?", reviewerID).
			Set("claimed_at = ?", now).
			Set("review_reason = NULL").
			Where("id = ?", appealID).
			Where("status IN (?, ?)", enum.AppealStatusRejected, enum.AppealStatusAccepted).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to reopen appeal: %w (appealID=%d)", err, appealID)
		}

		// Update timeline
		_, err = tx.NewUpdate().
			Model((*types.AppealTimeline)(nil)).
			Set("last_activity = ?", now).
			Set("last_viewed = ?", now).
			Where("id = ?", appealID).
			Where("timestamp = ?", timestamp).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to update appeal timeline: %w (appealID=%d)", err, appealID)
		}

		r.logger.Debug("Reopened appeal",
			zap.Int64("appealID", appealID))
		return nil
	})
}

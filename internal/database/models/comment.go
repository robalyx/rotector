package models

import (
	"context"
	"fmt"
	"time"

	"github.com/robalyx/rotector/internal/database/types"
	"github.com/uptrace/bun"
	"go.uber.org/zap"
)

// CommentModel handles database operations for comments.
type CommentModel struct {
	db     *bun.DB
	logger *zap.Logger
}

// NewComment creates a new comment model.
func NewComment(db *bun.DB, logger *zap.Logger) *CommentModel {
	return &CommentModel{
		db:     db,
		logger: logger.Named("db_comment"),
	}
}

// GetUserComments retrieves comments for a target user ID.
func (r *CommentModel) GetUserComments(ctx context.Context, targetID uint64) ([]*types.Comment, error) {
	var comments []*types.Comment
	err := r.db.NewSelect().
		TableExpr("user_comments").
		ColumnExpr("target_id, commenter_id, message, created_at, updated_at").
		Where("target_id = ?", targetID).
		Order("created_at DESC").
		Scan(ctx, &comments)
	if err != nil {
		return nil, fmt.Errorf("failed to get comments: %w", err)
	}
	return comments, nil
}

// UpsertUserComment inserts or updates a comment from a user.
func (r *CommentModel) UpsertUserComment(ctx context.Context, comment *types.UserComment) error {
	// Set timestamps
	now := time.Now()
	comment.CreatedAt = now
	comment.UpdatedAt = now

	// Upsert comment
	_, err := r.db.NewInsert().
		Model(comment).
		On("CONFLICT (target_id, commenter_id) DO UPDATE").
		Set("message = EXCLUDED.message").
		Set("updated_at = EXCLUDED.updated_at").
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to add/update comment: %w", err)
	}

	return nil
}

// DeleteUserComment deletes a comment by the commenter.
func (r *CommentModel) DeleteUserComment(ctx context.Context, targetID, commenterID uint64) error {
	_, err := r.db.NewDelete().
		Model((*types.UserComment)(nil)).
		Where("target_id = ?", targetID).
		Where("commenter_id = ?", commenterID). // Only allow deleting own comments
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete comment: %w", err)
	}
	return nil
}

// GetGroupComments retrieves comments for a target group ID.
func (r *CommentModel) GetGroupComments(ctx context.Context, targetID uint64) ([]*types.Comment, error) {
	var comments []*types.Comment
	err := r.db.NewSelect().
		TableExpr("group_comments").
		ColumnExpr("target_id, commenter_id, message, created_at, updated_at").
		Where("target_id = ?", targetID).
		Order("created_at DESC").
		Scan(ctx, &comments)
	if err != nil {
		return nil, fmt.Errorf("failed to get comments: %w", err)
	}
	return comments, nil
}

// UpsertGroupComment inserts or updates a comment from a user.
func (r *CommentModel) UpsertGroupComment(ctx context.Context, comment *types.GroupComment) error {
	// Set timestamps
	now := time.Now()
	comment.CreatedAt = now
	comment.UpdatedAt = now

	// Upsert comment
	_, err := r.db.NewInsert().
		Model(comment).
		On("CONFLICT (target_id, commenter_id) DO UPDATE").
		Set("message = EXCLUDED.message").
		Set("updated_at = EXCLUDED.updated_at").
		Set("is_deleted = false").
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to add/update comment: %w", err)
	}

	return nil
}

// DeleteGroupComment deletes a comment by the commenter.
func (r *CommentModel) DeleteGroupComment(ctx context.Context, targetID, commenterID uint64) error {
	_, err := r.db.NewDelete().
		Model((*types.GroupComment)(nil)).
		Where("target_id = ?", targetID).
		Where("commenter_id = ?", commenterID). // Only allow deleting own comments
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete comment: %w", err)
	}
	return nil
}

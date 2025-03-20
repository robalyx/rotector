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

// ReputationModel handles database operations for reputation records.
type ReputationModel struct {
	db     *bun.DB
	logger *zap.Logger
}

// NewReputation creates a new ReputationModel instance.
func NewReputation(db *bun.DB, logger *zap.Logger) *ReputationModel {
	return &ReputationModel{
		db:     db,
		logger: logger.Named("db_reputation"),
	}
}

// UpdateUserVotes updates the upvotes or downvotes count for a user.
//
// Deprecated: Use Service().Reputation().UpdateUserVotes() instead.
func (r *ReputationModel) UpdateUserVotes(ctx context.Context, userID uint64, isUpvote bool) error {
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var reputation types.UserReputation
		err := tx.NewSelect().
			Model(&reputation).
			Where("id = ?", userID).
			For("UPDATE").
			Scan(ctx)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("failed to get reputation: %w", err)
		}

		// Update vote counts
		if isUpvote {
			reputation.Upvotes++
		} else {
			reputation.Downvotes++
		}

		// Update reputation
		reputation.ID = userID
		reputation.Score = reputation.Upvotes - reputation.Downvotes
		reputation.UpdatedAt = time.Now()
		_, err = tx.NewInsert().
			Model(&reputation).
			On("CONFLICT (id) DO UPDATE").
			Set("upvotes = EXCLUDED.upvotes").
			Set("downvotes = EXCLUDED.downvotes").
			Set("score = EXCLUDED.score").
			Set("updated_at = EXCLUDED.updated_at").
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to update reputation: %w", err)
		}

		return nil
	})
}

// UpdateGroupVotes updates the upvotes or downvotes count for a group.
//
// Deprecated: Use Service().Reputation().UpdateGroupVotes() instead.
func (r *ReputationModel) UpdateGroupVotes(ctx context.Context, groupID uint64, isUpvote bool) error {
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var reputation types.GroupReputation
		err := tx.NewSelect().
			Model(&reputation).
			Where("id = ?", groupID).
			For("UPDATE").
			Scan(ctx)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("failed to get reputation: %w", err)
		}

		// Update vote counts
		if isUpvote {
			reputation.Upvotes++
		} else {
			reputation.Downvotes++
		}

		// Update reputation
		reputation.ID = groupID
		reputation.Score = reputation.Upvotes - reputation.Downvotes
		reputation.UpdatedAt = time.Now()
		_, err = tx.NewInsert().
			Model(&reputation).
			On("CONFLICT (id) DO UPDATE").
			Set("upvotes = EXCLUDED.upvotes").
			Set("downvotes = EXCLUDED.downvotes").
			Set("score = EXCLUDED.score").
			Set("updated_at = EXCLUDED.updated_at").
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to update reputation: %w", err)
		}

		return nil
	})
}

// GetUserReputation retrieves the reputation for a user.
func (r *ReputationModel) GetUserReputation(ctx context.Context, userID uint64) (types.Reputation, error) {
	var reputation types.UserReputation
	err := r.db.NewSelect().
		Model(&reputation).
		Where("id = ?", userID).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return types.Reputation{}, nil
		}
		return types.Reputation{}, fmt.Errorf("failed to get user reputation: %w", err)
	}
	return reputation.Reputation, nil
}

// GetGroupReputation retrieves the reputation for a group.
func (r *ReputationModel) GetGroupReputation(ctx context.Context, groupID uint64) (types.Reputation, error) {
	var reputation types.GroupReputation
	err := r.db.NewSelect().
		Model(&reputation).
		Where("id = ?", groupID).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return types.Reputation{}, nil
		}
		return types.Reputation{}, fmt.Errorf("failed to get group reputation: %w", err)
	}
	return reputation.Reputation, nil
}

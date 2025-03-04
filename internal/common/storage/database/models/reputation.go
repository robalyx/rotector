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

// ReputationModel handles database operations for reputation records.
type ReputationModel struct {
	db     *bun.DB
	votes  *VoteModel
	logger *zap.Logger
}

// NewReputation creates a new ReputationModel instance.
func NewReputation(db *bun.DB, votes *VoteModel, logger *zap.Logger) *ReputationModel {
	return &ReputationModel{
		db:     db,
		votes:  votes,
		logger: logger,
	}
}

// UpdateUserVotes updates the upvotes or downvotes count for a user in training mode.
func (r *ReputationModel) UpdateUserVotes(
	ctx context.Context, userID uint64, discordUserID uint64, isUpvote bool,
) error {
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
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
	if err != nil {
		return fmt.Errorf("failed to update user reputation: %w", err)
	}

	// Save the vote
	if err := r.votes.SaveVote(ctx, userID, discordUserID, isUpvote, enum.VoteTypeUser); err != nil {
		return fmt.Errorf("failed to save vote: %w", err)
	}

	return nil
}

// UpdateGroupVotes updates the upvotes or downvotes count for a group in training mode.
func (r *ReputationModel) UpdateGroupVotes(
	ctx context.Context, groupID uint64, discordUserID uint64, isUpvote bool,
) error {
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
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
	if err != nil {
		return fmt.Errorf("failed to update group reputation: %w", err)
	}

	// Save the vote
	if err := r.votes.SaveVote(ctx, groupID, discordUserID, isUpvote, enum.VoteTypeGroup); err != nil {
		return fmt.Errorf("failed to save vote: %w", err)
	}

	return nil
}

// GetUserReputation retrieves the reputation for a user.
func (r *ReputationModel) GetUserReputation(ctx context.Context, userID uint64) (*types.Reputation, error) {
	var reputation types.UserReputation
	err := r.db.NewSelect().
		Model(&reputation).
		Where("id = ?", userID).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return &types.Reputation{}, nil
		}
		return nil, fmt.Errorf("failed to get user reputation: %w", err)
	}
	return &reputation.Reputation, nil
}

// GetGroupReputation retrieves the reputation for a group.
func (r *ReputationModel) GetGroupReputation(ctx context.Context, groupID uint64) (*types.Reputation, error) {
	var reputation types.GroupReputation
	err := r.db.NewSelect().
		Model(&reputation).
		Where("id = ?", groupID).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return &types.Reputation{}, nil
		}
		return nil, fmt.Errorf("failed to get group reputation: %w", err)
	}
	return &reputation.Reputation, nil
}

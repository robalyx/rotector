package service

import (
	"context"
	"fmt"

	"github.com/robalyx/rotector/internal/common/storage/database/models"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
	"go.uber.org/zap"
)

// ReputationService handles reputation-related business logic.
type ReputationService struct {
	model  *models.ReputationModel
	votes  *models.VoteModel
	logger *zap.Logger
}

// NewReputation creates a new reputation service.
func NewReputation(
	model *models.ReputationModel,
	votes *models.VoteModel,
	logger *zap.Logger,
) *ReputationService {
	return &ReputationService{
		model:  model,
		votes:  votes,
		logger: logger.Named("reputation_service"),
	}
}

// UpdateUserVotes updates the upvotes or downvotes count for a user in training mode.
func (s *ReputationService) UpdateUserVotes(ctx context.Context, userID, discordUserID uint64, isUpvote bool) error {
	// Update reputation in the model
	if err := s.model.UpdateUserVotes(ctx, userID, isUpvote); err != nil {
		return fmt.Errorf("failed to update user reputation: %w", err)
	}

	// Save the vote
	if err := s.votes.SaveVote(ctx, userID, discordUserID, isUpvote, enum.VoteTypeUser); err != nil {
		return fmt.Errorf("failed to save vote: %w", err)
	}

	return nil
}

// UpdateGroupVotes updates the upvotes or downvotes count for a group in training mode.
func (s *ReputationService) UpdateGroupVotes(ctx context.Context, groupID, discordUserID uint64, isUpvote bool) error {
	// Update reputation in the model
	if err := s.model.UpdateGroupVotes(ctx, groupID, isUpvote); err != nil {
		return fmt.Errorf("failed to update group reputation: %w", err)
	}

	// Save the vote
	if err := s.votes.SaveVote(ctx, groupID, discordUserID, isUpvote, enum.VoteTypeGroup); err != nil {
		return fmt.Errorf("failed to save vote: %w", err)
	}

	return nil
}

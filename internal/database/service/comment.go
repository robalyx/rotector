package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/robalyx/rotector/internal/database/models"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/pkg/utils"
	"go.uber.org/zap"
)

var (
	ErrInvalidComment    = errors.New("invalid comment message")
	ErrCommentTooSimilar = errors.New("comment too similar to existing ones")
)

// CommentService handles comment-related business logic.
type CommentService struct {
	model      *models.CommentModel
	normalizer *utils.TextNormalizer
	logger     *zap.Logger
}

// NewComment creates a new comment service.
func NewComment(model *models.CommentModel, logger *zap.Logger) *CommentService {
	return &CommentService{
		model:      model,
		normalizer: utils.NewTextNormalizer(),
		logger:     logger.Named("comment_service"),
	}
}

// AddUserComment adds a new comment for a user with spam prevention.
func (s *CommentService) AddUserComment(ctx context.Context, comment *types.UserComment) error {
	return s.addComment(ctx, comment.TargetID, comment.CommenterID, comment.Message, true)
}

// AddGroupComment adds a new comment for a group with spam prevention.
func (s *CommentService) AddGroupComment(ctx context.Context, comment *types.GroupComment) error {
	return s.addComment(ctx, comment.TargetID, comment.CommenterID, comment.Message, false)
}

// addComment handles the common logic for adding comments to users or groups.
func (s *CommentService) addComment(ctx context.Context, targetID int64, commenterID uint64, message string, isUserComment bool) error {
	// Get existing comments for this target
	var (
		existingComments []*types.Comment
		err              error
	)

	if isUserComment {
		existingComments, err = s.model.GetUserComments(ctx, targetID)
	} else {
		existingComments, err = s.model.GetGroupComments(ctx, targetID)
	}

	if err != nil {
		return fmt.Errorf("failed to get existing comments: %w", err)
	}

	// Validate comment text contains only allowed characters
	if !utils.ValidateCommentText(message) {
		return ErrInvalidComment
	}

	// Normalize the new comment message
	normalizedNew := s.normalizer.Normalize(message)
	if normalizedNew == "" {
		return ErrInvalidComment
	}

	// Check for similar comments from other users
	for _, existing := range existingComments {
		// Skip user's own comment when checking for similarity
		if existing.CommenterID == commenterID {
			continue
		}

		normalizedExisting := s.normalizer.Normalize(existing.Message)
		if normalizedExisting == "" {
			continue
		}

		// Check if comments are too similar
		if strings.Contains(normalizedNew, normalizedExisting) ||
			strings.Contains(normalizedExisting, normalizedNew) {
			return ErrCommentTooSimilar
		}
	}

	// Save the comment
	comment := types.Comment{
		TargetID:    targetID,
		CommenterID: commenterID,
		Message:     message,
	}

	if isUserComment {
		err = s.model.UpsertUserComment(ctx, &types.UserComment{Comment: comment})
	} else {
		err = s.model.UpsertGroupComment(ctx, &types.GroupComment{Comment: comment})
	}

	if err != nil {
		return fmt.Errorf("failed to save comment: %w", err)
	}

	return nil
}

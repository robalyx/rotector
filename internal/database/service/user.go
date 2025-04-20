package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/robalyx/rotector/internal/database/models"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/database/types/enum"
	"go.uber.org/zap"
)

// UserService handles user-related business logic.
type UserService struct {
	model      *models.UserModel
	activity   *models.ActivityModel
	reputation *models.ReputationModel
	votes      *models.VoteModel
	logger     *zap.Logger
}

// NewUser creates a new user service.
func NewUser(
	model *models.UserModel,
	activity *models.ActivityModel,
	reputation *models.ReputationModel,
	votes *models.VoteModel,
	logger *zap.Logger,
) *UserService {
	return &UserService{
		model:      model,
		activity:   activity,
		reputation: reputation,
		votes:      votes,
		logger:     logger.Named("user_service"),
	}
}

// ConfirmUser moves a user from other user tables to confirmed_users.
func (s *UserService) ConfirmUser(ctx context.Context, user *types.ReviewUser, reviewerID uint64) error {
	// Set reviewer ID
	user.ReviewerID = reviewerID

	// Move user to confirmed table
	if err := s.model.ConfirmUser(ctx, user); err != nil {
		return err
	}

	// Verify votes for the user
	if err := s.votes.VerifyVotes(ctx, user.ID, true, enum.VoteTypeUser); err != nil {
		s.logger.Error("Failed to verify votes", zap.Error(err))
		return err
	}

	return nil
}

// ClearUser moves a user from other user tables to cleared_users.
func (s *UserService) ClearUser(ctx context.Context, user *types.ReviewUser, reviewerID uint64) error {
	// Set reviewer ID
	user.ReviewerID = reviewerID

	// Move user to cleared table
	if err := s.model.ClearUser(ctx, user); err != nil {
		return err
	}

	// Verify votes for the user
	if err := s.votes.VerifyVotes(ctx, user.ID, false, enum.VoteTypeUser); err != nil {
		s.logger.Error("Failed to verify votes", zap.Error(err))
		return err
	}

	return nil
}

// GetUserByID retrieves a user by ID with reputation information.
func (s *UserService) GetUserByID(
	ctx context.Context, userID string, fields types.UserField,
) (*types.ReviewUser, error) {
	// Get the user from the model layer
	user, err := s.model.GetUserByID(ctx, userID, fields)
	if err != nil {
		return nil, err
	}

	// Get reputation if requested
	if fields.HasReputation() {
		reputation, err := s.reputation.GetUserReputation(ctx, user.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to get user reputation: %w", err)
		}
		user.Reputation = reputation
	}

	return user, nil
}

// GetUserToReview finds a user to review based on the sort method and target mode.
func (s *UserService) GetUserToReview(
	ctx context.Context,
	sortBy enum.ReviewSortBy,
	targetMode enum.ReviewTargetMode,
	reviewerID uint64,
) (*types.ReviewUser, error) {
	// Get recently reviewed user IDs
	recentIDs, err := s.activity.GetRecentlyReviewedIDs(ctx, reviewerID, false, 50)
	if err != nil {
		s.logger.Error("Failed to get recently reviewed user IDs", zap.Error(err))
		recentIDs = []uint64{} // Continue without filtering if there's an error
	}

	// Define models in priority order based on target mode
	var models []any
	switch targetMode {
	case enum.ReviewTargetModeFlagged:
		models = []any{
			&types.FlaggedUser{},
			&types.ConfirmedUser{},
			&types.ClearedUser{},
		}
	case enum.ReviewTargetModeConfirmed:
		models = []any{
			&types.ConfirmedUser{},
			&types.FlaggedUser{},
			&types.ClearedUser{},
		}
	case enum.ReviewTargetModeCleared:
		models = []any{
			&types.ClearedUser{},
			&types.FlaggedUser{},
			&types.ConfirmedUser{},
		}
	}

	// Try each model in order until we find a user
	for _, model := range models {
		result, err := s.model.GetNextToReview(ctx, model, sortBy, recentIDs)
		if err == nil {
			// Get reputation
			reputation, err := s.reputation.GetUserReputation(ctx, result.ID)
			if err != nil {
				return nil, fmt.Errorf("failed to get user reputation: %w", err)
			}
			result.Reputation = reputation
			return result, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}

	return nil, types.ErrNoUsersToReview
}

// SaveUsers handles the business logic for saving users.
func (s *UserService) SaveUsers(ctx context.Context, users map[uint64]*types.User) error {
	// Get list of user IDs to check
	userIDs := make([]uint64, 0, len(users))
	for id := range users {
		userIDs = append(userIDs, id)
	}

	// Get existing users with all their data
	existingUsers, err := s.model.GetUsersByIDs(
		ctx,
		userIDs,
		types.UserFieldBasic|types.UserFieldTimestamps|types.UserFieldReasons,
	)
	if err != nil {
		return fmt.Errorf("failed to get existing users: %w", err)
	}

	// Group users by their status and merge reasons
	flaggedUsers, confirmedUsers, clearedUsers := s.groupUsersByStatus(users, existingUsers)

	// Save the grouped users
	if err := s.model.SaveUsersByStatus(ctx, flaggedUsers, confirmedUsers, clearedUsers); err != nil {
		return err
	}

	s.logger.Debug("Successfully saved users",
		zap.Int("totalUsers", len(users)),
		zap.Int("flaggedUsers", len(flaggedUsers)),
		zap.Int("confirmedUsers", len(confirmedUsers)),
		zap.Int("clearedUsers", len(clearedUsers)))

	return nil
}

// groupUsersByStatus groups users by their status and merges reasons.
func (s *UserService) groupUsersByStatus(
	users map[uint64]*types.User, existingUsers map[uint64]*types.ReviewUser,
) ([]*types.FlaggedUser, []*types.ConfirmedUser, []*types.ClearedUser) {
	var flaggedUsers []*types.FlaggedUser
	var confirmedUsers []*types.ConfirmedUser
	var clearedUsers []*types.ClearedUser

	for id, user := range users {
		// Generate UUID for new users
		if user.UUID == uuid.Nil {
			user.UUID = uuid.New()
		}

		// Handle reasons merging and determine status
		status := enum.UserTypeFlagged
		existingUser, ok := existingUsers[id]
		if ok {
			status = existingUser.Status

			// Create new reasons map if it doesn't exist
			if user.Reasons == nil {
				user.Reasons = make(types.Reasons[enum.UserReasonType])
			}

			// Copy over existing reasons, only adding new ones
			for reasonType, reason := range existingUser.Reasons {
				if _, exists := user.Reasons[reasonType]; !exists {
					user.Reasons[reasonType] = reason
				}
			}
		}

		// Group users by their target tables
		switch status {
		case enum.UserTypeConfirmed:
			confirmedUsers = append(confirmedUsers, &types.ConfirmedUser{
				User:       *user,
				VerifiedAt: existingUser.VerifiedAt,
				ReviewerID: existingUser.ReviewerID,
			})
		case enum.UserTypeFlagged:
			flaggedUsers = append(flaggedUsers, &types.FlaggedUser{
				User: *user,
			})
		case enum.UserTypeCleared:
			clearedUsers = append(clearedUsers, &types.ClearedUser{
				User:       *user,
				ClearedAt:  existingUser.ClearedAt,
				ReviewerID: existingUser.ReviewerID,
			})
		}
	}

	return flaggedUsers, confirmedUsers, clearedUsers
}

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
	tracking   *models.TrackingModel
	logger     *zap.Logger
}

// NewUser creates a new user service.
func NewUser(
	model *models.UserModel,
	activity *models.ActivityModel,
	reputation *models.ReputationModel,
	votes *models.VoteModel,
	tracking *models.TrackingModel,
	logger *zap.Logger,
) *UserService {
	return &UserService{
		model:      model,
		activity:   activity,
		reputation: reputation,
		votes:      votes,
		tracking:   tracking,
		logger:     logger.Named("user_service"),
	}
}

// ConfirmUser moves a user to confirmed status and creates a verification record.
func (s *UserService) ConfirmUser(ctx context.Context, user *types.ReviewUser, reviewerID uint64) error {
	// Set reviewer ID
	user.ReviewerID = reviewerID
	user.Status = enum.UserTypeConfirmed

	// Update user status and create verification record
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

// ClearUser moves a user to cleared status and creates a clearance record.
func (s *UserService) ClearUser(ctx context.Context, user *types.ReviewUser, reviewerID uint64) error {
	// Set reviewer ID
	user.ReviewerID = reviewerID
	user.Status = enum.UserTypeCleared

	// Update user status and create clearance record
	if err := s.model.ClearUser(ctx, user); err != nil {
		return err
	}

	// Remove user from all group tracking
	if err := s.tracking.RemoveUserFromAllGroups(ctx, user.ID); err != nil {
		s.logger.Error("Failed to remove user from group tracking", zap.Error(err))
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

	// Determine target status based on mode
	var targetStatus enum.UserType
	switch targetMode {
	case enum.ReviewTargetModeFlagged:
		targetStatus = enum.UserTypeFlagged
	case enum.ReviewTargetModeConfirmed:
		targetStatus = enum.UserTypeConfirmed
	case enum.ReviewTargetModeCleared:
		targetStatus = enum.UserTypeCleared
	}

	// Get next user to review
	result, err := s.model.GetNextToReview(ctx, targetStatus, sortBy, recentIDs)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// If no users found with primary status, try other statuses in order
			var fallbackStatuses []enum.UserType
			switch targetMode {
			case enum.ReviewTargetModeFlagged:
				fallbackStatuses = []enum.UserType{enum.UserTypeConfirmed, enum.UserTypeCleared}
			case enum.ReviewTargetModeConfirmed:
				fallbackStatuses = []enum.UserType{enum.UserTypeFlagged, enum.UserTypeCleared}
			case enum.ReviewTargetModeCleared:
				fallbackStatuses = []enum.UserType{enum.UserTypeFlagged, enum.UserTypeConfirmed}
			}

			for _, status := range fallbackStatuses {
				result, err = s.model.GetNextToReview(ctx, status, sortBy, recentIDs)
				if err == nil {
					break
				}
				if !errors.Is(err, sql.ErrNoRows) {
					return nil, err
				}
			}

			if err != nil {
				return nil, types.ErrNoUsersToReview
			}
		} else {
			return nil, err
		}
	}

	// Get reputation
	reputation, err := s.reputation.GetUserReputation(ctx, result.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user reputation: %w", err)
	}
	result.Reputation = reputation

	return result, nil
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

	// Prepare users for saving
	usersToSave := make([]*types.User, 0, len(users))
	for id, user := range users {
		// Generate UUID for new users
		if user.UUID == uuid.Nil {
			user.UUID = uuid.New()
		}

		// Handle reasons merging and determine status
		existingUser, ok := existingUsers[id]
		if ok {
			user.Status = existingUser.Status

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
		} else {
			user.Status = enum.UserTypeFlagged
		}

		usersToSave = append(usersToSave, user)
	}

	// Save the users
	if err := s.model.SaveUsers(ctx, usersToSave); err != nil {
		return err
	}

	s.logger.Debug("Successfully saved users",
		zap.Int("totalUsers", len(users)))

	return nil
}

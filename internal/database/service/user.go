package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	apiTypes "github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/robalyx/rotector/internal/database/models"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/database/types/enum"
	"github.com/sourcegraph/conc/pool"
	"github.com/uptrace/bun"
	"go.uber.org/zap"
)

// UserService handles user-related business logic.
type UserService struct {
	db         *bun.DB
	model      *models.UserModel
	activity   *models.ActivityModel
	reputation *models.ReputationModel
	votes      *models.VoteModel
	tracking   *models.TrackingModel
	logger     *zap.Logger
}

// NewUser creates a new user service.
func NewUser(
	db *bun.DB,
	model *models.UserModel,
	activity *models.ActivityModel,
	reputation *models.ReputationModel,
	votes *models.VoteModel,
	tracking *models.TrackingModel,
	logger *zap.Logger,
) *UserService {
	return &UserService{
		db:         db,
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
	if err := s.tracking.RemoveUsersFromAllGroups(ctx, []uint64{user.ID}); err != nil {
		s.logger.Error("Failed to remove user from group tracking", zap.Error(err))
		return err
	}

	// Remove user and their outfits from asset tracking
	if err := s.tracking.RemoveUsersFromAssetTracking(ctx, []uint64{user.ID}); err != nil {
		s.logger.Error("Failed to remove user from outfit asset tracking", zap.Error(err))
		return err
	}

	// Remove user from game tracking
	if err := s.tracking.RemoveUsersFromGameTracking(ctx, []uint64{user.ID}); err != nil {
		s.logger.Error("Failed to remove user from game tracking", zap.Error(err))
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

	// Get relationships if requested
	if fields.Has(types.UserFieldRelationships) {
		relationships, err := s.GetUserRelationships(ctx, user.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to get user relationships: %w", err)
		}

		user.Groups = relationships.Groups
		user.Outfits = relationships.Outfits
		user.Friends = relationships.Friends
		user.Favorites = relationships.Favorites
		user.Games = relationships.Games
		user.Inventory = relationships.Inventory
		user.Badges = relationships.Badges
	}

	return user, nil
}

// GetUserToReview finds a user to review based on the sort method and target mode.
func (s *UserService) GetUserToReview(
	ctx context.Context, sortBy enum.ReviewSortBy, targetMode enum.ReviewTargetMode, reviewerID uint64,
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
		if errors.Is(err, types.ErrNoUsersToReview) {
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
				if !errors.Is(err, types.ErrNoUsersToReview) {
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

	// Get relationships
	relationships, err := s.GetUserRelationships(ctx, result.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user relationships: %w", err)
	}

	// Update relationships
	result.Groups = relationships.Groups
	result.Outfits = relationships.Outfits
	result.Friends = relationships.Friends
	result.Favorites = relationships.Favorites
	result.Games = relationships.Games
	result.Inventory = relationships.Inventory
	result.Badges = relationships.Badges

	return result, nil
}

// GetUserRelationships fetches all relationships for a user.
func (s *UserService) GetUserRelationships(ctx context.Context, userID uint64) (*types.ReviewUser, error) {
	var result types.ReviewUser
	var mu sync.Mutex
	p := pool.New().WithContext(ctx).WithCancelOnError()

	// Fetch groups in parallel
	p.Go(func(ctx context.Context) error {
		groups, err := s.model.GetUserGroups(ctx, userID)
		if err != nil {
			return fmt.Errorf("failed to get user groups: %w", err)
		}
		mu.Lock()
		result.Groups = groups
		mu.Unlock()
		return nil
	})

	// Fetch outfits in parallel
	p.Go(func(ctx context.Context) error {
		outfits, outfitAssets, err := s.model.GetUserOutfits(ctx, userID)
		if err != nil {
			return fmt.Errorf("failed to get user outfits: %w", err)
		}
		mu.Lock()
		result.Outfits = outfits
		result.OutfitAssets = outfitAssets
		mu.Unlock()
		return nil
	})

	// Fetch friends in parallel
	p.Go(func(ctx context.Context) error {
		friends, err := s.model.GetUserFriends(ctx, userID)
		if err != nil {
			return fmt.Errorf("failed to get user friends: %w", err)
		}
		mu.Lock()
		result.Friends = friends
		mu.Unlock()
		return nil
	})

	// Fetch favorites in parallel
	p.Go(func(ctx context.Context) error {
		favorites, err := s.model.GetUserFavorites(ctx, userID)
		if err != nil {
			return fmt.Errorf("failed to get user favorites: %w", err)
		}
		mu.Lock()
		result.Favorites = favorites
		mu.Unlock()
		return nil
	})

	// Fetch games in parallel
	p.Go(func(ctx context.Context) error {
		games, err := s.model.GetUserGames(ctx, userID)
		if err != nil {
			return fmt.Errorf("failed to get user games: %w", err)
		}
		mu.Lock()
		result.Games = games
		mu.Unlock()
		return nil
	})

	// Fetch inventory in parallel
	p.Go(func(ctx context.Context) error {
		inventory, err := s.model.GetUserInventory(ctx, userID)
		if err != nil {
			return fmt.Errorf("failed to get user inventory: %w", err)
		}
		mu.Lock()
		result.Inventory = inventory
		mu.Unlock()
		return nil
	})

	// Wait for all goroutines
	if err := p.Wait(); err != nil {
		return nil, fmt.Errorf("failed to get user relationships: %w", err)
	}

	return &result, nil
}

// SaveUsers handles the business logic for saving users.
func (s *UserService) SaveUsers(ctx context.Context, users map[uint64]*types.ReviewUser) error {
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
	usersToSave := make([]*types.ReviewUser, 0, len(users))
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
	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// First save core user data
		if err := s.model.SaveUsers(ctx, tx, usersToSave); err != nil {
			return err
		}

		// Prepare batch data structures
		userGroups := make(map[uint64][]*apiTypes.UserGroupRoles)
		userOutfits := make(map[uint64][]*apiTypes.Outfit)
		userOutfitAssets := make(map[uint64]map[uint64][]*apiTypes.AssetV2)
		userAssets := make(map[uint64][]*apiTypes.AssetV2)
		userFriends := make(map[uint64][]*apiTypes.ExtendedFriend)
		userFavorites := make(map[uint64][]*apiTypes.Game)
		userGames := make(map[uint64][]*apiTypes.Game)
		userInventory := make(map[uint64][]*apiTypes.InventoryAsset)

		// Collect all relationships
		for _, user := range usersToSave {
			if len(user.Groups) > 0 {
				userGroups[user.ID] = user.Groups
			}
			if len(user.Outfits) > 0 {
				userOutfits[user.ID] = user.Outfits
				if len(user.OutfitAssets) > 0 {
					userOutfitAssets[user.ID] = user.OutfitAssets
				}
			}
			if len(user.CurrentAssets) > 0 {
				userAssets[user.ID] = user.CurrentAssets
			}
			if len(user.Friends) > 0 {
				userFriends[user.ID] = user.Friends
			}
			if len(user.Favorites) > 0 {
				userFavorites[user.ID] = user.Favorites
			}
			if len(user.Games) > 0 {
				userGames[user.ID] = user.Games
			}
			if len(user.Inventory) > 0 {
				userInventory[user.ID] = user.Inventory
			}
		}

		// Save all relationships
		if err := s.model.SaveUserGroups(ctx, tx, userGroups); err != nil {
			return err
		}

		if err := s.model.SaveUserOutfits(ctx, tx, userOutfits, userOutfitAssets); err != nil {
			return err
		}

		if err := s.model.SaveUserAssets(ctx, tx, userAssets); err != nil {
			return err
		}

		if err := s.model.SaveUserFriends(ctx, tx, userFriends); err != nil {
			return err
		}

		if err := s.model.SaveUserFavorites(ctx, tx, userFavorites); err != nil {
			return err
		}

		if err := s.model.SaveUserGames(ctx, tx, userGames); err != nil {
			return err
		}

		if err := s.model.SaveUserInventory(ctx, tx, userInventory); err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to save users: %w", err)
	}

	s.logger.Debug("Successfully saved users",
		zap.Int("totalUsers", len(users)))

	return nil
}

// DeleteUsers removes multiple users and all their associated data from the database.
func (s *UserService) DeleteUsers(ctx context.Context, userIDs []uint64) (int64, error) {
	if len(userIDs) == 0 {
		return 0, nil
	}

	var totalAffected int64
	err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Remove users from tracking
		if err := s.tracking.RemoveUsersFromAllGroups(ctx, userIDs); err != nil {
			s.logger.Error("Failed to remove users from group tracking", zap.Error(err))
			return err
		}

		if err := s.tracking.RemoveUsersFromAssetTracking(ctx, userIDs); err != nil {
			s.logger.Error("Failed to remove users from asset tracking", zap.Error(err))
			return err
		}

		if err := s.tracking.RemoveUsersFromGameTracking(ctx, userIDs); err != nil {
			s.logger.Error("Failed to remove users from game tracking", zap.Error(err))
			return err
		}

		// Delete core user data
		affected, err := s.model.DeleteUsers(ctx, userIDs)
		if err != nil {
			return fmt.Errorf("failed to delete users core data: %w", err)
		}
		totalAffected += affected

		// Delete all relationships and their unreferenced info
		affected, err = s.model.DeleteUserGroups(ctx, tx, userIDs)
		if err != nil {
			return fmt.Errorf("failed to delete user groups: %w", err)
		}
		totalAffected += affected

		affected, err = s.model.DeleteUserOutfits(ctx, tx, userIDs)
		if err != nil {
			return fmt.Errorf("failed to delete user outfits: %w", err)
		}
		totalAffected += affected

		affected, err = s.model.DeleteUserFriends(ctx, tx, userIDs)
		if err != nil {
			return fmt.Errorf("failed to delete user friends: %w", err)
		}
		totalAffected += affected

		affected, err = s.model.DeleteUserFavorites(ctx, tx, userIDs)
		if err != nil {
			return fmt.Errorf("failed to delete user favorites: %w", err)
		}
		totalAffected += affected

		affected, err = s.model.DeleteUserGames(ctx, tx, userIDs)
		if err != nil {
			return fmt.Errorf("failed to delete user games: %w", err)
		}
		totalAffected += affected

		affected, err = s.model.DeleteUserInventory(ctx, tx, userIDs)
		if err != nil {
			return fmt.Errorf("failed to delete user inventory: %w", err)
		}
		totalAffected += affected

		return nil
	})
	if err != nil {
		return 0, err
	}

	s.logger.Debug("Deleted users and all associated data",
		zap.Int("userCount", len(userIDs)),
		zap.Int64("totalAffected", totalAffected))

	return totalAffected, nil
}

// DeleteUser removes a single user and all associated data from the database.
func (s *UserService) DeleteUser(ctx context.Context, userID uint64) (bool, error) {
	affected, err := s.DeleteUsers(ctx, []uint64{userID})
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

// PurgeOldClearedUsers removes cleared users older than the cutoff date.
func (s *UserService) PurgeOldClearedUsers(ctx context.Context, cutoffDate time.Time) (int, error) {
	// Get users to delete
	userIDs, err := s.model.GetOldClearedUsers(ctx, cutoffDate)
	if err != nil {
		return 0, fmt.Errorf("failed to get old cleared users: %w", err)
	}

	if len(userIDs) == 0 {
		return 0, nil
	}

	// Delete users in bulk
	affected, err := s.model.DeleteUsers(ctx, userIDs)
	if err != nil {
		return 0, fmt.Errorf("failed to delete users: %w", err)
	}

	s.logger.Debug("Purged old cleared users",
		zap.Int("count", len(userIDs)),
		zap.Int64("affectedRows", affected),
		zap.Time("cutoffDate", cutoffDate))

	return len(userIDs), nil
}

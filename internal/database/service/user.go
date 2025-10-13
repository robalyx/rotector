package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	apiTypes "github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/robalyx/rotector/internal/database/dbretry"
	"github.com/robalyx/rotector/internal/database/models"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/database/types/enum"
	"github.com/sourcegraph/conc/pool"
	"github.com/uptrace/bun"
	"go.uber.org/zap"
)

// UserService handles user-related business logic.
type UserService struct {
	db       *bun.DB
	model    *models.UserModel
	activity *models.ActivityModel
	tracking *models.TrackingModel
	logger   *zap.Logger
}

// NewUser creates a new user service.
func NewUser(
	db *bun.DB,
	model *models.UserModel,
	activity *models.ActivityModel,
	tracking *models.TrackingModel,
	logger *zap.Logger,
) *UserService {
	return &UserService{
		db:       db,
		model:    model,
		activity: activity,
		tracking: tracking,
		logger:   logger.Named("user_service"),
	}
}

// ConfirmUser moves a user to confirmed status and creates a verification record.
func (s *UserService) ConfirmUser(ctx context.Context, user *types.ReviewUser, reviewerID uint64) error {
	return s.ConfirmUsers(ctx, []*types.ReviewUser{user}, reviewerID)
}

// ConfirmUsers moves multiple users to confirmed status and creates verification records.
func (s *UserService) ConfirmUsers(ctx context.Context, users []*types.ReviewUser, reviewerID uint64) error {
	if len(users) == 0 {
		return nil
	}

	// Set reviewer ID and status for all users
	for _, user := range users {
		user.ReviewerID = reviewerID
		user.Status = enum.UserTypeConfirmed
	}

	// Update user statuses and create verification records
	if err := s.model.ConfirmUsers(ctx, users); err != nil {
		return err
	}

	return nil
}

// ClearUser moves a user to cleared status and creates a clearance record.
func (s *UserService) ClearUser(ctx context.Context, user *types.ReviewUser, reviewerID uint64) error {
	return dbretry.Transaction(ctx, s.db, func(ctx context.Context, tx bun.Tx) error {
		return s.ClearUserWithTx(ctx, tx, user, reviewerID)
	})
}

// ClearUserWithTx moves a user to cleared status and creates a clearance record using the provided transaction.
func (s *UserService) ClearUserWithTx(ctx context.Context, tx bun.Tx, user *types.ReviewUser, reviewerID uint64) error {
	// Set reviewer ID
	user.ReviewerID = reviewerID
	user.Status = enum.UserTypeCleared

	// Update user status and create clearance record
	if err := s.model.ClearUserWithTx(ctx, tx, user); err != nil {
		return err
	}

	// Remove user from all group tracking
	if err := s.tracking.RemoveUsersFromAllGroupsWithTx(ctx, tx, []int64{user.ID}); err != nil {
		s.logger.Error("Failed to remove user from group tracking", zap.Error(err))
		return err
	}

	// Remove user and their outfits from asset tracking
	if err := s.tracking.RemoveUsersFromAssetTrackingWithTx(ctx, tx, []int64{user.ID}); err != nil {
		s.logger.Error("Failed to remove user from outfit asset tracking", zap.Error(err))
		return err
	}

	// Remove user from game tracking
	if err := s.tracking.RemoveUsersFromGameTrackingWithTx(ctx, tx, []int64{user.ID}); err != nil {
		s.logger.Error("Failed to remove user from game tracking", zap.Error(err))
		return err
	}

	return nil
}

// UpdateToPastOffender updates users to past offender status when they become clean.
func (s *UserService) UpdateToPastOffender(ctx context.Context, userIDs []int64) error {
	if len(userIDs) == 0 {
		return nil
	}

	return dbretry.Transaction(ctx, s.db, func(ctx context.Context, tx bun.Tx) error {
		// Update users to past offender status
		if err := s.model.UpdateUsersToPastOffender(ctx, tx, userIDs); err != nil {
			return err
		}

		// Remove users from all tracking
		if err := s.tracking.RemoveUsersFromAllGroupsWithTx(ctx, tx, userIDs); err != nil {
			s.logger.Error("Failed to remove users from group tracking", zap.Error(err))
			return err
		}

		if err := s.tracking.RemoveUsersFromAssetTrackingWithTx(ctx, tx, userIDs); err != nil {
			s.logger.Error("Failed to remove users from asset tracking", zap.Error(err))
			return err
		}

		if err := s.tracking.RemoveUsersFromGameTrackingWithTx(ctx, tx, userIDs); err != nil {
			s.logger.Error("Failed to remove users from game tracking", zap.Error(err))
			return err
		}

		return nil
	})
}

// GetUserByID retrieves a user by either their numeric ID or UUID.
func (s *UserService) GetUserByID(
	ctx context.Context, userID string, fields types.UserField,
) (*types.ReviewUser, error) {
	// Get the user from the model layer
	user, err := s.model.GetUserByID(ctx, userID, fields)
	if err != nil {
		return nil, err
	}

	// Get specific relationships if requested
	relationshipFields := fields & types.UserFieldRelationships
	if relationshipFields != 0 {
		relationships := s.GetUsersRelationships(ctx, []int64{user.ID}, relationshipFields)
		if rel, exists := relationships[user.ID]; exists {
			if fields.Has(types.UserFieldGroups) {
				user.Groups = rel.Groups
			}

			if fields.Has(types.UserFieldOutfits) {
				user.Outfits = rel.Outfits
				user.OutfitAssets = rel.OutfitAssets
			}

			if fields.Has(types.UserFieldCurrentAssets) {
				user.CurrentAssets = rel.CurrentAssets
			}

			if fields.Has(types.UserFieldFriends) {
				user.Friends = rel.Friends
			}

			if fields.Has(types.UserFieldFavorites) {
				user.Favorites = rel.Favorites
			}

			if fields.Has(types.UserFieldGames) {
				user.Games = rel.Games
			}

			if fields.Has(types.UserFieldInventory) {
				user.Inventory = rel.Inventory
			}

			if fields.Has(types.UserFieldBadges) {
				user.Badges = rel.Badges
			}
		}
	}

	return user, nil
}

// GetUsersByIDs retrieves multiple users by their IDs with specified fields.
func (s *UserService) GetUsersByIDs(
	ctx context.Context, userIDs []int64, fields types.UserField,
) (map[int64]*types.ReviewUser, error) {
	if len(userIDs) == 0 {
		return make(map[int64]*types.ReviewUser), nil
	}

	// Get users from the model layer
	users, err := s.model.GetUsersByIDs(ctx, userIDs, fields)
	if err != nil {
		return nil, err
	}

	// Get specific relationships if requested
	relationshipFields := fields & types.UserFieldRelationships
	if relationshipFields != 0 {
		relationships := s.GetUsersRelationships(ctx, userIDs, relationshipFields)
		for userID, user := range users {
			if rel, exists := relationships[userID]; exists {
				if fields.Has(types.UserFieldGroups) {
					user.Groups = rel.Groups
				}

				if fields.Has(types.UserFieldOutfits) {
					user.Outfits = rel.Outfits
					user.OutfitAssets = rel.OutfitAssets
				}

				if fields.Has(types.UserFieldCurrentAssets) {
					user.CurrentAssets = rel.CurrentAssets
				}

				if fields.Has(types.UserFieldFriends) {
					user.Friends = rel.Friends
				}

				if fields.Has(types.UserFieldFavorites) {
					user.Favorites = rel.Favorites
				}

				if fields.Has(types.UserFieldGames) {
					user.Games = rel.Games
				}

				if fields.Has(types.UserFieldInventory) {
					user.Inventory = rel.Inventory
				}

				if fields.Has(types.UserFieldBadges) {
					user.Badges = rel.Badges
				}
			}
		}
	}

	return users, nil
}

// GetUserToReview finds a user to review based on the sort method and target mode.
func (s *UserService) GetUserToReview(
	ctx context.Context, sortBy enum.ReviewSortBy, targetMode enum.ReviewTargetMode, reviewerID uint64,
) (*types.ReviewUser, error) {
	// Get recently reviewed user IDs
	recentIDs, err := s.activity.GetRecentlyReviewedIDs(ctx, reviewerID, false, 50)
	if err != nil {
		s.logger.Error("Failed to get recently reviewed user IDs", zap.Error(err))

		recentIDs = []int64{} // Continue without filtering if there's an error
	}

	// Determine target status based on mode
	var targetStatus enum.UserType
	switch targetMode {
	case enum.ReviewTargetModeFlagged:
		targetStatus = enum.UserTypeFlagged
	case enum.ReviewTargetModeConfirmed:
		targetStatus = enum.UserTypeConfirmed
	case enum.ReviewTargetModeMixed:
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
			case enum.ReviewTargetModeMixed:
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
func (s *UserService) GetUserRelationships(ctx context.Context, userID int64) (*types.ReviewUser, error) {
	results := s.GetUsersRelationships(ctx, []int64{userID}, types.UserFieldRelationships)
	if result, exists := results[userID]; exists {
		return result, nil
	}

	return &types.ReviewUser{}, nil
}

// GetUsersRelationships fetches only the requested relationships for multiple users.
func (s *UserService) GetUsersRelationships(
	ctx context.Context, userIDs []int64, relationshipFields types.UserField,
) map[int64]*types.ReviewUser {
	if len(userIDs) == 0 {
		return make(map[int64]*types.ReviewUser)
	}

	result := make(map[int64]*types.ReviewUser)
	for _, userID := range userIDs {
		result[userID] = &types.ReviewUser{}
	}

	var mu sync.Mutex

	p := pool.New().WithContext(ctx).WithCancelOnError()

	// Fetch groups in parallel if requested
	if relationshipFields.Has(types.UserFieldGroups) {
		p.Go(func(ctx context.Context) error {
			groups, err := s.model.GetUsersGroups(ctx, userIDs)
			if err != nil {
				return fmt.Errorf("failed to get users groups: %w", err)
			}

			mu.Lock()

			for userID, userGroups := range groups {
				result[userID].Groups = userGroups
			}

			mu.Unlock()

			return nil
		})
	}

	// Fetch outfits in parallel if requested
	if relationshipFields.Has(types.UserFieldOutfits) {
		p.Go(func(ctx context.Context) error {
			outfits, err := s.model.GetUsersOutfits(ctx, userIDs)
			if err != nil {
				return fmt.Errorf("failed to get users outfits: %w", err)
			}

			mu.Lock()

			for userID, userOutfits := range outfits {
				result[userID].Outfits = userOutfits.Outfits
				result[userID].OutfitAssets = userOutfits.OutfitAssets
			}

			mu.Unlock()

			return nil
		})
	}

	// Fetch friends in parallel if requested
	if relationshipFields.Has(types.UserFieldFriends) {
		p.Go(func(ctx context.Context) error {
			friends, err := s.model.GetUsersFriends(ctx, userIDs)
			if err != nil {
				return fmt.Errorf("failed to get users friends: %w", err)
			}

			mu.Lock()

			for userID, userFriends := range friends {
				result[userID].Friends = userFriends
			}

			mu.Unlock()

			return nil
		})
	}

	// Fetch favorites in parallel if requested
	if relationshipFields.Has(types.UserFieldFavorites) {
		p.Go(func(ctx context.Context) error {
			favorites, err := s.model.GetUsersFavorites(ctx, userIDs)
			if err != nil {
				return fmt.Errorf("failed to get users favorites: %w", err)
			}

			mu.Lock()

			for userID, userFavorites := range favorites {
				result[userID].Favorites = userFavorites
			}

			mu.Unlock()

			return nil
		})
	}

	// Fetch games in parallel if requested
	if relationshipFields.Has(types.UserFieldGames) {
		p.Go(func(ctx context.Context) error {
			games, err := s.model.GetUsersGames(ctx, userIDs)
			if err != nil {
				return fmt.Errorf("failed to get users games: %w", err)
			}

			mu.Lock()

			for userID, userGames := range games {
				result[userID].Games = userGames
			}

			mu.Unlock()

			return nil
		})
	}

	// Fetch inventory in parallel if requested
	if relationshipFields.Has(types.UserFieldInventory) {
		p.Go(func(ctx context.Context) error {
			inventory, err := s.model.GetUsersInventory(ctx, userIDs)
			if err != nil {
				return fmt.Errorf("failed to get users inventory: %w", err)
			}

			mu.Lock()

			for userID, userInventory := range inventory {
				result[userID].Inventory = userInventory
			}

			mu.Unlock()

			return nil
		})
	}

	// Fetch current assets in parallel if requested
	if relationshipFields.Has(types.UserFieldCurrentAssets) {
		p.Go(func(ctx context.Context) error {
			assets, err := s.model.GetUsersAssets(ctx, userIDs)
			if err != nil {
				return fmt.Errorf("failed to get users assets: %w", err)
			}

			mu.Lock()

			for userID, userAssets := range assets {
				result[userID].CurrentAssets = userAssets
			}

			mu.Unlock()

			return nil
		})
	}

	// Note: UserFieldBadges is not implemented in the model layer yet

	// Wait for all goroutines
	if err := p.Wait(); err != nil {
		s.logger.Error("Failed to get users relationships", zap.Error(err))
		return make(map[int64]*types.ReviewUser)
	}

	return result
}

// SaveUsers handles the business logic for saving users.
func (s *UserService) SaveUsers(ctx context.Context, users map[int64]*types.ReviewUser) error {
	// Get list of user IDs to check
	userIDs := make([]int64, 0, len(users))
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

		// Set engine version for user processing
		user.EngineVersion = types.CurrentEngineVersion

		// Determine status based on whether user exists and has reasons
		if existingUser, ok := existingUsers[id]; ok {
			// Past offenders who get flagged again should return to flagged status
			if existingUser.Status == enum.UserTypePastOffender && len(user.Reasons) > 0 {
				user.Status = enum.UserTypeFlagged
			} else {
				// Keep existing status for already flagged/confirmed users
				user.Status = existingUser.Status
			}
		} else {
			// New users start as flagged
			user.Status = enum.UserTypeFlagged
		}

		// Create empty reasons map if nil (reasons will be completely replaced)
		if user.Reasons == nil {
			user.Reasons = make(types.Reasons[enum.UserReasonType])
		}

		usersToSave = append(usersToSave, user)
	}

	// Save the users
	err = dbretry.Transaction(ctx, s.db, func(ctx context.Context, tx bun.Tx) error {
		// First save core user data
		if err := s.model.SaveUsers(ctx, tx, usersToSave); err != nil {
			return err
		}

		// Prepare batch data structures
		userGroups := make(map[int64][]*apiTypes.UserGroupRoles)
		userOutfits := make(map[int64][]*apiTypes.Outfit)
		userOutfitAssets := make(map[int64]map[int64][]*apiTypes.AssetV2)
		userAssets := make(map[int64][]*apiTypes.AssetV2)
		userFriends := make(map[int64][]*apiTypes.ExtendedFriend)
		userFavorites := make(map[int64][]*apiTypes.Game)
		userGames := make(map[int64][]*apiTypes.Game)
		userInventory := make(map[int64][]*apiTypes.InventoryAsset)

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

		if err := s.model.SaveUserGames(ctx, tx, userGames); err != nil {
			return err
		}

		if err := s.model.SaveUserFavorites(ctx, tx, userFavorites); err != nil {
			return err
		}

		if err := s.model.SaveUserAssets(ctx, tx, userAssets); err != nil {
			return err
		}

		if err := s.model.SaveUserOutfits(ctx, tx, userOutfits, userOutfitAssets); err != nil {
			return err
		}

		if err := s.model.SaveUserGroups(ctx, tx, userGroups); err != nil {
			return err
		}

		if err := s.model.SaveUserFriends(ctx, tx, userFriends); err != nil {
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
func (s *UserService) DeleteUsers(ctx context.Context, userIDs []int64) (int64, error) {
	if len(userIDs) == 0 {
		return 0, nil
	}

	var totalAffected int64

	err := dbretry.Transaction(ctx, s.db, func(ctx context.Context, tx bun.Tx) error {
		var err error

		totalAffected, err = s.DeleteUsersWithTx(ctx, tx, userIDs)

		return err
	})
	if err != nil {
		return 0, err
	}

	return totalAffected, nil
}

// DeleteUsersWithTx removes multiple users and all their associated data from the database using the provided transaction.
func (s *UserService) DeleteUsersWithTx(ctx context.Context, tx bun.Tx, userIDs []int64) (int64, error) {
	if len(userIDs) == 0 {
		return 0, nil
	}

	var totalAffected int64

	// Remove users from tracking
	if err := s.tracking.RemoveUsersFromAllGroupsWithTx(ctx, tx, userIDs); err != nil {
		s.logger.Error("Failed to remove users from group tracking", zap.Error(err))
		return 0, err
	}

	if err := s.tracking.RemoveUsersFromAssetTrackingWithTx(ctx, tx, userIDs); err != nil {
		s.logger.Error("Failed to remove users from asset tracking", zap.Error(err))
		return 0, err
	}

	if err := s.tracking.RemoveUsersFromGameTrackingWithTx(ctx, tx, userIDs); err != nil {
		s.logger.Error("Failed to remove users from game tracking", zap.Error(err))
		return 0, err
	}

	// Delete core user data
	affected, err := s.model.DeleteUsersWithTx(ctx, tx, userIDs)
	if err != nil {
		return 0, fmt.Errorf("failed to delete users core data: %w", err)
	}

	totalAffected += affected

	// Delete all relationships and their unreferenced info
	affected, err = s.model.DeleteUserGroups(ctx, tx, userIDs)
	if err != nil {
		return 0, fmt.Errorf("failed to delete user groups: %w", err)
	}

	totalAffected += affected

	affected, err = s.model.DeleteUserOutfits(ctx, tx, userIDs)
	if err != nil {
		return 0, fmt.Errorf("failed to delete user outfits: %w", err)
	}

	totalAffected += affected

	affected, err = s.model.DeleteUserFriends(ctx, tx, userIDs)
	if err != nil {
		return 0, fmt.Errorf("failed to delete user friends: %w", err)
	}

	totalAffected += affected

	affected, err = s.model.DeleteUserFavorites(ctx, tx, userIDs)
	if err != nil {
		return 0, fmt.Errorf("failed to delete user favorites: %w", err)
	}

	totalAffected += affected

	affected, err = s.model.DeleteUserGames(ctx, tx, userIDs)
	if err != nil {
		return 0, fmt.Errorf("failed to delete user games: %w", err)
	}

	totalAffected += affected

	affected, err = s.model.DeleteUserInventory(ctx, tx, userIDs)
	if err != nil {
		return 0, fmt.Errorf("failed to delete user inventory: %w", err)
	}

	totalAffected += affected

	s.logger.Debug("Deleted users and all associated data",
		zap.Int("userCount", len(userIDs)),
		zap.Int64("totalAffected", totalAffected))

	return totalAffected, nil
}

// DeleteUser removes a single user and all associated data from the database.
func (s *UserService) DeleteUser(ctx context.Context, userID int64) (bool, error) {
	affected, err := s.DeleteUsers(ctx, []int64{userID})
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

// GetFlaggedUsersWithProfileReasons returns flagged users that have profile reasons with
// confidence above the specified threshold, including all their reason data.
func (s *UserService) GetFlaggedUsersWithProfileReasons(
	ctx context.Context, confidenceThreshold float64, limit int,
) ([]*types.ReviewUser, error) {
	// Get user IDs from model layer
	userIDs, err := s.model.GetFlaggedUsersWithProfileReasons(ctx, confidenceThreshold, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get user IDs with profile reasons: %w", err)
	}

	if len(userIDs) == 0 {
		s.logger.Debug("No flagged users with profile reasons found",
			zap.Float64("confidenceThreshold", confidenceThreshold))

		return []*types.ReviewUser{}, nil
	}

	// Get full user data with reasons using existing service method
	userMap, err := s.GetUsersByIDs(ctx, userIDs, types.UserFieldBasic|types.UserFieldReasons)
	if err != nil {
		return nil, fmt.Errorf("failed to get users by IDs: %w", err)
	}

	// Convert map to slice, preserving order
	users := make([]*types.ReviewUser, 0, len(userIDs))
	for _, userID := range userIDs {
		if user, exists := userMap[userID]; exists {
			users = append(users, user)
		}
	}

	s.logger.Debug("Retrieved flagged users with profile reasons",
		zap.Float64("confidenceThreshold", confidenceThreshold),
		zap.Int("limit", limit),
		zap.Int("requestedCount", len(userIDs)),
		zap.Int("returnedCount", len(users)))

	return users, nil
}

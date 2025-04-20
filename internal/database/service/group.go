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

// GroupService handles group-related business logic.
type GroupService struct {
	model      *models.GroupModel
	activity   *models.ActivityModel
	reputation *models.ReputationModel
	votes      *models.VoteModel
	logger     *zap.Logger
}

// NewGroup creates a new group service.
func NewGroup(
	model *models.GroupModel,
	activity *models.ActivityModel,
	reputation *models.ReputationModel,
	votes *models.VoteModel,
	logger *zap.Logger,
) *GroupService {
	return &GroupService{
		model:      model,
		activity:   activity,
		reputation: reputation,
		votes:      votes,
		logger:     logger.Named("group_service"),
	}
}

// ConfirmGroup moves a group from other group tables to confirmed_groups.
func (s *GroupService) ConfirmGroup(ctx context.Context, group *types.ReviewGroup, reviewerID uint64) error {
	// Set reviewer ID
	group.ReviewerID = reviewerID

	// Move group to confirmed table
	if err := s.model.ConfirmGroup(ctx, group); err != nil {
		return err
	}

	// Verify votes for the group
	if err := s.votes.VerifyVotes(ctx, group.ID, true, enum.VoteTypeGroup); err != nil {
		s.logger.Error("Failed to verify votes", zap.Error(err))
		return err
	}

	return nil
}

// ClearGroup moves a group from other group tables to cleared_groups.
func (s *GroupService) ClearGroup(ctx context.Context, group *types.ReviewGroup, reviewerID uint64) error {
	// Set reviewer ID
	group.ReviewerID = reviewerID

	// Move group to cleared table
	if err := s.model.ClearGroup(ctx, group); err != nil {
		return err
	}

	// Verify votes for the group
	if err := s.votes.VerifyVotes(ctx, group.ID, false, enum.VoteTypeGroup); err != nil {
		s.logger.Error("Failed to verify votes", zap.Error(err))
		return err
	}

	return nil
}

// GetGroupByID retrieves a group by ID with reputation information.
func (s *GroupService) GetGroupByID(
	ctx context.Context, groupID string, fields types.GroupField,
) (*types.ReviewGroup, error) {
	// Get the group from the model layer
	group, err := s.model.GetGroupByID(ctx, groupID, fields)
	if err != nil {
		return nil, err
	}

	// Get reputation if requested
	if fields.HasReputation() {
		reputation, err := s.reputation.GetGroupReputation(ctx, group.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to get group reputation: %w", err)
		}
		group.Reputation = reputation
	}

	return group, nil
}

// GetGroupToReview finds a group to review based on the sort method and target mode.
func (s *GroupService) GetGroupToReview(
	ctx context.Context,
	sortBy enum.ReviewSortBy,
	targetMode enum.ReviewTargetMode,
	reviewerID uint64,
) (*types.ReviewGroup, error) {
	// Get recently reviewed group IDs
	recentIDs, err := s.activity.GetRecentlyReviewedIDs(ctx, reviewerID, true, 3)
	if err != nil {
		s.logger.Error("Failed to get recently reviewed group IDs", zap.Error(err))
		recentIDs = []uint64{} // Continue without filtering if there's an error
	}

	// Define models in priority order based on target mode
	var models []any
	switch targetMode {
	case enum.ReviewTargetModeFlagged:
		models = []any{
			&types.FlaggedGroup{},
			&types.ConfirmedGroup{},
			&types.ClearedGroup{},
		}
	case enum.ReviewTargetModeConfirmed:
		models = []any{
			&types.ConfirmedGroup{},
			&types.FlaggedGroup{},
			&types.ClearedGroup{},
		}
	case enum.ReviewTargetModeCleared:
		models = []any{
			&types.ClearedGroup{},
			&types.FlaggedGroup{},
			&types.ConfirmedGroup{},
		}
	}

	// Try each model in order until we find a group
	for _, model := range models {
		result, err := s.model.GetNextToReview(ctx, model, sortBy, recentIDs)
		if err == nil {
			// Get reputation
			reputation, err := s.reputation.GetGroupReputation(ctx, result.ID)
			if err != nil {
				return nil, fmt.Errorf("failed to get group reputation: %w", err)
			}
			result.Reputation = reputation
			return result, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}

	return nil, types.ErrNoGroupsToReview
}

// SaveGroups handles the business logic for saving groups.
func (s *GroupService) SaveGroups(ctx context.Context, groups map[uint64]*types.Group) error {
	// Get list of group IDs to check
	groupIDs := make([]uint64, 0, len(groups))
	for id := range groups {
		groupIDs = append(groupIDs, id)
	}

	// Get existing groups with all their data
	existingGroups, err := s.model.GetGroupsByIDs(
		ctx,
		groupIDs,
		types.GroupFieldBasic|types.GroupFieldTimestamps|types.GroupFieldReasons,
	)
	if err != nil {
		return fmt.Errorf("failed to get existing groups: %w", err)
	}

	// Group groups by their status and merge reasons
	flaggedGroups, confirmedGroups, clearedGroups := s.groupGroupsByStatus(groups, existingGroups)

	// Save the grouped groups
	if err := s.model.SaveGroupsByStatus(ctx, flaggedGroups, confirmedGroups, clearedGroups); err != nil {
		return err
	}

	s.logger.Debug("Successfully saved groups",
		zap.Int("totalGroups", len(groups)),
		zap.Int("flaggedGroups", len(flaggedGroups)),
		zap.Int("confirmedGroups", len(confirmedGroups)),
		zap.Int("clearedGroups", len(clearedGroups)))

	return nil
}

// groupGroupsByStatus groups by their status and merges reasons.
func (s *GroupService) groupGroupsByStatus(
	groups map[uint64]*types.Group, existingGroups map[uint64]*types.ReviewGroup,
) ([]*types.FlaggedGroup, []*types.ConfirmedGroup, []*types.ClearedGroup) {
	var flaggedGroups []*types.FlaggedGroup
	var confirmedGroups []*types.ConfirmedGroup
	var clearedGroups []*types.ClearedGroup

	for id, group := range groups {
		// Generate UUID for new groups
		if group.UUID == uuid.Nil {
			group.UUID = uuid.New()
		}

		// Handle reasons merging and determine status
		status := enum.GroupTypeFlagged
		existingGroup, ok := existingGroups[id]
		if ok {
			status = existingGroup.Status

			// Create new reasons map if it doesn't exist
			if group.Reasons == nil {
				group.Reasons = make(types.Reasons[enum.GroupReasonType])
			}

			// Copy over existing reasons, only adding new ones
			for reasonType, reason := range existingGroup.Reasons {
				if _, exists := group.Reasons[reasonType]; !exists {
					group.Reasons[reasonType] = reason
				}
			}
		}

		// Group groups by their target tables
		switch status {
		case enum.GroupTypeConfirmed:
			confirmedGroups = append(confirmedGroups, &types.ConfirmedGroup{
				Group:      *group,
				VerifiedAt: existingGroup.VerifiedAt,
				ReviewerID: existingGroup.ReviewerID,
			})
		case enum.GroupTypeFlagged:
			flaggedGroups = append(flaggedGroups, &types.FlaggedGroup{
				Group: *group,
			})
		case enum.GroupTypeCleared:
			clearedGroups = append(clearedGroups, &types.ClearedGroup{
				Group:      *group,
				ClearedAt:  existingGroup.ClearedAt,
				ReviewerID: existingGroup.ReviewerID,
			})
		}
	}

	return flaggedGroups, confirmedGroups, clearedGroups
}

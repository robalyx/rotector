package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/robalyx/rotector/internal/database/dbretry"
	"github.com/robalyx/rotector/internal/database/models"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/database/types/enum"
	"github.com/uptrace/bun"
	"go.uber.org/zap"
)

// GroupService handles group-related business logic.
type GroupService struct {
	db       *bun.DB
	model    *models.GroupModel
	activity *models.ActivityModel
	logger   *zap.Logger
}

// NewGroup creates a new group service.
func NewGroup(
	db *bun.DB,
	model *models.GroupModel,
	activity *models.ActivityModel,
	logger *zap.Logger,
) *GroupService {
	return &GroupService{
		db:       db,
		model:    model,
		activity: activity,
		logger:   logger.Named("group_service"),
	}
}

// ConfirmGroup moves a group to confirmed status and creates a verification record.
func (s *GroupService) ConfirmGroup(ctx context.Context, group *types.ReviewGroup, reviewerID uint64) error {
	// Set reviewer ID
	group.ReviewerID = reviewerID
	group.Status = enum.GroupTypeConfirmed

	// Update group status and create verification record
	if err := s.model.ConfirmGroup(ctx, group); err != nil {
		return err
	}

	return nil
}

// MixGroup moves a group to mixed status and creates a mixed classification record.
func (s *GroupService) MixGroup(ctx context.Context, group *types.ReviewGroup, reviewerID uint64) error {
	// Set reviewer ID
	group.ReviewerID = reviewerID
	group.Status = enum.GroupTypeMixed

	// Update group status and create mixed classification record
	if err := s.model.MixGroup(ctx, group); err != nil {
		return err
	}

	return nil
}

// GetGroupToReview finds a group to review based on the sort method and target mode.
func (s *GroupService) GetGroupToReview(
	ctx context.Context, sortBy enum.ReviewSortBy, targetMode enum.ReviewTargetMode, reviewerID uint64,
) (*types.ReviewGroup, error) {
	// Get recently reviewed group IDs
	recentIDs, err := s.activity.GetRecentlyReviewedIDs(ctx, reviewerID, true, 50)
	if err != nil {
		s.logger.Error("Failed to get recently reviewed group IDs", zap.Error(err))

		recentIDs = []int64{} // Continue without filtering if there's an error
	}

	// Determine target status based on mode
	var targetStatus enum.GroupType
	switch targetMode {
	case enum.ReviewTargetModeFlagged:
		targetStatus = enum.GroupTypeFlagged
	case enum.ReviewTargetModeConfirmed:
		targetStatus = enum.GroupTypeConfirmed
	case enum.ReviewTargetModeMixed:
		targetStatus = enum.GroupTypeMixed
	}

	// Get next group to review
	result, err := s.model.GetNextToReview(ctx, targetStatus, sortBy, recentIDs)
	if err != nil {
		if errors.Is(err, types.ErrNoGroupsToReview) {
			// If no groups found with primary status, try other statuses in order
			var fallbackStatuses []enum.GroupType
			switch targetMode {
			case enum.ReviewTargetModeFlagged:
				fallbackStatuses = []enum.GroupType{enum.GroupTypeConfirmed, enum.GroupTypeMixed}
			case enum.ReviewTargetModeConfirmed:
				fallbackStatuses = []enum.GroupType{enum.GroupTypeFlagged, enum.GroupTypeMixed}
			case enum.ReviewTargetModeMixed:
				fallbackStatuses = []enum.GroupType{enum.GroupTypeFlagged, enum.GroupTypeConfirmed}
			}

			for _, status := range fallbackStatuses {
				result, err = s.model.GetNextToReview(ctx, status, sortBy, recentIDs)
				if err == nil {
					break
				}

				if !errors.Is(err, types.ErrNoGroupsToReview) {
					return nil, err
				}
			}

			if err != nil {
				return nil, types.ErrNoGroupsToReview
			}
		} else {
			return nil, err
		}
	}

	return result, nil
}

// SaveGroups handles the business logic for saving groups.
func (s *GroupService) SaveGroups(ctx context.Context, groups map[int64]*types.ReviewGroup) error {
	// Get list of group IDs to check
	groupIDs := make([]int64, 0, len(groups))
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

	// Prepare groups for saving
	groupsToSave := make([]*types.ReviewGroup, 0, len(groups))
	for id, group := range groups {
		// Generate UUID for new groups
		if group.UUID == uuid.Nil {
			group.UUID = uuid.New()
		}

		// Handle reasons merging and determine status
		existingGroup, ok := existingGroups[id]
		if ok {
			group.Status = existingGroup.Status

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
		} else {
			group.Status = enum.GroupTypeFlagged
		}

		groupsToSave = append(groupsToSave, group)
	}

	// Save the groups
	err = dbretry.Transaction(ctx, s.db, func(ctx context.Context, tx bun.Tx) error {
		// Save groups with their reasons
		if err := s.model.SaveGroups(ctx, tx, groupsToSave); err != nil {
			return err
		}

		// NOTE: any additional logic can be added here

		return nil
	})
	if err != nil {
		return err
	}

	s.logger.Debug("Successfully saved groups",
		zap.Int("totalGroups", len(groups)))

	return nil
}

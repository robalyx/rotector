package checker

import (
	"fmt"

	"github.com/rotector/rotector/internal/common/database"
	"github.com/rotector/rotector/internal/common/fetcher"
	"go.uber.org/zap"
)

// GroupChecker handles the checking of user groups by comparing them against
// a database of known inappropriate groups.
type GroupChecker struct {
	db     *database.Database
	logger *zap.Logger
}

// NewGroupChecker creates a GroupChecker with database access for looking up
// flagged group information.
func NewGroupChecker(db *database.Database, logger *zap.Logger) *GroupChecker {
	return &GroupChecker{
		db:     db,
		logger: logger,
	}
}

// ProcessUserGroups checks if a user belongs to multiple flagged groups.
// The confidence score increases with the number of flagged groups relative
// to total group membership.
func (gc *GroupChecker) ProcessUserGroups(userInfo *fetcher.Info) (*database.User, bool, error) {
	// Skip users with no group memberships
	if len(userInfo.Groups) == 0 {
		return nil, false, nil
	}

	// Extract group IDs for batch lookup
	groupIDs := make([]uint64, len(userInfo.Groups))
	for i, group := range userInfo.Groups {
		groupIDs[i] = group.Group.ID
	}

	// Check database for flagged groups
	flaggedGroupIDs, err := gc.db.Groups().CheckConfirmedGroups(groupIDs)
	if err != nil {
		gc.logger.Error("Error checking flagged groups", zap.Error(err), zap.Uint64("userID", userInfo.ID))
		return nil, false, err
	}

	// Auto-flag users in 2 or more flagged groups
	if len(flaggedGroupIDs) >= 2 {
		user := &database.User{
			ID:            userInfo.ID,
			Name:          userInfo.Name,
			DisplayName:   userInfo.DisplayName,
			Description:   userInfo.Description,
			CreatedAt:     userInfo.CreatedAt,
			Reason:        fmt.Sprintf("Member of %d flagged groups", len(flaggedGroupIDs)),
			Groups:        userInfo.Groups,
			FlaggedGroups: flaggedGroupIDs,
			Confidence:    float64(len(flaggedGroupIDs)) / float64(len(userInfo.Groups)),
			LastUpdated:   userInfo.LastUpdated,
		}

		gc.logger.Info("User automatically flagged",
			zap.Uint64("userID", userInfo.ID),
			zap.Uint64s("flaggedGroupIDs", flaggedGroupIDs))

		return user, true, nil
	}

	return nil, false, nil
}

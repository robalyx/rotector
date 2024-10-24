package fetcher

import (
	"fmt"

	"github.com/rotector/rotector/internal/common/database"
	"go.uber.org/zap"
)

// GroupChecker handles checking of user groups.
type GroupChecker struct {
	db     *database.Database
	logger *zap.Logger
}

// NewGroupChecker creates a new GroupChecker instance.
func NewGroupChecker(db *database.Database, logger *zap.Logger) *GroupChecker {
	return &GroupChecker{
		db:     db,
		logger: logger,
	}
}

// CheckUserGroups checks if a user belongs to any flagged groups and returns the result.
func (gc *GroupChecker) CheckUserGroups(userInfo *Info) (*database.User, bool, error) {
	// Get group IDs
	groupIDs := make([]uint64, len(userInfo.Groups))
	for i, group := range userInfo.Groups {
		groupIDs[i] = group.Group.ID
	}

	// Check if user belongs to any flagged groups
	flaggedGroupIDs, err := gc.db.Groups().CheckConfirmedGroups(groupIDs)
	if err != nil {
		gc.logger.Error("Error checking flagged groups", zap.Error(err), zap.Uint64("userID", userInfo.ID))
		return nil, false, err
	}

	if len(flaggedGroupIDs) >= 3 {
		// User belongs to 3 or more flagged groups, flag automatically
		user := database.User{
			ID:            userInfo.ID,
			Name:          userInfo.Name,
			DisplayName:   userInfo.DisplayName,
			Description:   userInfo.Description,
			CreatedAt:     userInfo.CreatedAt,
			Reason:        fmt.Sprintf("Member of %d flagged groups", len(flaggedGroupIDs)),
			Groups:        userInfo.Groups,
			FlaggedGroups: flaggedGroupIDs,
			Confidence:    float64(len(flaggedGroupIDs)) / float64(len(userInfo.Groups)),
		}

		gc.logger.Info("User automatically flagged",
			zap.Uint64("userID", userInfo.ID),
			zap.Uint64s("flaggedGroupIDs", flaggedGroupIDs))

		return &user, true, nil
	}

	return nil, false, nil
}

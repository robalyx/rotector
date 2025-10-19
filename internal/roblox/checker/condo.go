package checker

import (
	"fmt"
	"strings"

	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/database/types/enum"
	"go.uber.org/zap"
)

// CondoCheckerParams contains all the parameters needed for condo checker processing.
type CondoCheckerParams struct {
	Users              []*types.ReviewUser                          `json:"users"`
	ReasonsMap         map[int64]types.Reasons[enum.UserReasonType] `json:"reasonsMap"`
	ConfirmedGroupsMap map[int64]map[int64]*types.ReviewGroup       `json:"confirmedGroupsMap"`
}

// CondoChecker detects users involved in condo group activity based on confirmed
// group purpose reasons containing "condo" references.
type CondoChecker struct {
	logger *zap.Logger
}

// NewCondoChecker creates a CondoChecker.
func NewCondoChecker(logger *zap.Logger) *CondoChecker {
	return &CondoChecker{
		logger: logger.Named("condo_checker"),
	}
}

// ProcessUsers checks multiple users for condo group activity and updates reasonsMap.
func (c *CondoChecker) ProcessUsers(params *CondoCheckerParams) {
	existingFlags := len(params.ReasonsMap)

	// Track users flagged for condo activity
	flaggedCount := 0

	// Process each user
	for _, userInfo := range params.Users {
		// Check for condo group activity
		shouldFlag, condoCount, details := c.detectCondoActivity(userInfo, params.ConfirmedGroupsMap[userInfo.ID])
		if !shouldFlag {
			continue
		}

		// Build reason message
		reasonMessage := fmt.Sprintf("Member of %d confirmed condo groups.", condoCount)

		// Calculate confidence based on number of condo groups
		confidence := c.calculateConfidence(condoCount)

		// Add condo reason to reasons map
		if _, exists := params.ReasonsMap[userInfo.ID]; !exists {
			params.ReasonsMap[userInfo.ID] = make(types.Reasons[enum.UserReasonType])
		}

		params.ReasonsMap[userInfo.ID].AddWithSource(enum.UserReasonTypeCondo, &types.Reason{
			Message:    reasonMessage,
			Confidence: confidence,
		}, "Groups")

		flaggedCount++

		c.logger.Debug("User flagged for condo group activity",
			zap.Int64("userID", userInfo.ID),
			zap.Int("condoGroups", condoCount),
			zap.Float64("confidence", confidence),
			zap.String("details", details))
	}

	c.logger.Info("Finished processing condo groups",
		zap.Int("totalUsers", len(params.Users)),
		zap.Int("flaggedUsers", flaggedCount),
		zap.Int("newFlags", len(params.ReasonsMap)-existingFlags))
}

// detectCondoActivity checks if a user should be flagged for condo group activity.
// Returns true if user meets criteria, along with count and details for logging.
func (c *CondoChecker) detectCondoActivity(
	userInfo *types.ReviewUser,
	confirmedGroups map[int64]*types.ReviewGroup,
) (shouldFlag bool, condoCount int, details string) {
	// Build map of group ID to member count from user's groups
	memberCountMap := make(map[int64]int64)
	for _, groupRole := range userInfo.Groups {
		memberCountMap[groupRole.Group.ID] = groupRole.Group.MemberCount
	}

	// Track condo groups and their details
	condoGroupIDs := make([]int64, 0)
	smallGroupCount := 0

	// Check each confirmed group for condo-related purpose reasons
	for groupID, reviewGroup := range confirmedGroups {
		// Check if group has a purpose reason
		purposeReason, hasPurpose := reviewGroup.Reasons[enum.GroupReasonTypePurpose]
		if !hasPurpose || purposeReason == nil {
			continue
		}

		// Check if "condo" appears in the purpose reason (case-insensitive)
		if strings.Contains(strings.ToLower(purposeReason.Message), "condo") {
			condoGroupIDs = append(condoGroupIDs, groupID)

			// Check if this is a small group (< 600 members)
			if memberCount, exists := memberCountMap[groupID]; exists && memberCount < 600 {
				smallGroupCount++
			}
		}
	}

	condoCount = len(condoGroupIDs)

	// Determine if user should be flagged
	if condoCount >= 3 || (condoCount >= 2 && smallGroupCount > 0) {
		shouldFlag = true

		// Build details string
		if smallGroupCount > 0 {
			details = fmt.Sprintf("Found %d condo groups (including %d with <600 members)", condoCount, smallGroupCount)
		} else {
			details = fmt.Sprintf("Found %d condo groups", condoCount)
		}
	}

	return shouldFlag, condoCount, details
}

// calculateConfidence computes a confidence score based on the number of condo groups.
func (c *CondoChecker) calculateConfidence(condoCount int) float64 {
	switch {
	case condoCount >= 5:
		return 0.95
	case condoCount >= 4:
		return 0.90
	case condoCount >= 3:
		return 0.85
	default:
		return 0.75
	}
}

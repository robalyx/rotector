package checker

import (
	"context"
	"fmt"
	"strings"

	"github.com/robalyx/rotector/internal/database"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/database/types/enum"
	"github.com/robalyx/rotector/internal/setup"
	"go.uber.org/zap"
)

// CondoCheckerParams contains all the parameters needed for condo checker processing.
type CondoCheckerParams struct {
	Users              []*types.ReviewUser                          `json:"users"`
	ReasonsMap         map[int64]types.Reasons[enum.UserReasonType] `json:"reasonsMap"`
	ConfirmedGroupsMap map[int64]map[int64]*types.ReviewGroup       `json:"confirmedGroupsMap"`
}

// CondoChecker detects users involved in condo activity.
type CondoChecker struct {
	db     database.Client
	logger *zap.Logger
}

// NewCondoChecker creates a CondoChecker.
func NewCondoChecker(app *setup.App, logger *zap.Logger) *CondoChecker {
	return &CondoChecker{
		db:     app.DB,
		logger: logger.Named("condo_checker"),
	}
}

// ProcessUsers checks multiple users for condo activity.
func (c *CondoChecker) ProcessUsers(ctx context.Context, params *CondoCheckerParams) {
	existingFlags := len(params.ReasonsMap)

	// Track users flagged by each method
	discordFlaggedCount := 0
	groupsFlaggedCount := 0

	// Step 1: Check for Discord server membership
	for _, userInfo := range params.Users {
		shouldFlag, serverCount, confidence := c.detectDiscordActivity(ctx, userInfo.ID)
		if !shouldFlag {
			continue
		}

		// Build reason message
		reasonMessage := fmt.Sprintf("Discord user with linked Roblox account detected in %d+ condo servers", serverCount)

		// Add condo reason with Discord source
		if _, exists := params.ReasonsMap[userInfo.ID]; !exists {
			params.ReasonsMap[userInfo.ID] = make(types.Reasons[enum.UserReasonType])
		}

		params.ReasonsMap[userInfo.ID].AddWithSource(enum.UserReasonTypeCondo, &types.Reason{
			Message:    reasonMessage,
			Confidence: confidence,
		}, "Discord")

		discordFlaggedCount++

		c.logger.Debug("User flagged for Discord condo server activity",
			zap.Int64("userID", userInfo.ID),
			zap.Int("serverCount", serverCount),
			zap.Float64("confidence", confidence))
	}

	// Step 2: Check for condo group membership
	for _, userInfo := range params.Users {
		shouldFlag, condoCount, details := c.detectGroupActivity(userInfo, params.ConfirmedGroupsMap[userInfo.ID])
		if !shouldFlag {
			continue
		}

		// Build reason message
		reasonMessage := fmt.Sprintf("Member of %d confirmed condo groups", condoCount)

		// Calculate confidence based on number of condo groups
		confidence := c.calculateGroupConfidence(condoCount)

		// Add condo reason with Groups source
		if _, exists := params.ReasonsMap[userInfo.ID]; !exists {
			params.ReasonsMap[userInfo.ID] = make(types.Reasons[enum.UserReasonType])
		}

		params.ReasonsMap[userInfo.ID].AddWithSource(enum.UserReasonTypeCondo, &types.Reason{
			Message:    reasonMessage,
			Confidence: confidence,
		}, "Groups")

		groupsFlaggedCount++

		c.logger.Debug("User flagged for condo group activity",
			zap.Int64("userID", userInfo.ID),
			zap.Int("condoGroups", condoCount),
			zap.Float64("confidence", confidence),
			zap.String("details", details))
	}

	c.logger.Info("Finished processing condo detection",
		zap.Int("totalUsers", len(params.Users)),
		zap.Int("discordFlags", discordFlaggedCount),
		zap.Int("groupFlags", groupsFlaggedCount),
		zap.Int("newFlags", len(params.ReasonsMap)-existingFlags))
}

// detectDiscordActivity checks if a user should be flagged based on Discord server membership.
// Returns true if user meets criteria (3+ servers), along with count and confidence.
func (c *CondoChecker) detectDiscordActivity(
	ctx context.Context, robloxUserID int64,
) (shouldFlag bool, serverCount int, confidence float64) {
	// Get Discord server count for this Roblox user
	count, err := c.db.Model().Sync().GetDiscordServerCountByRobloxID(ctx, robloxUserID)
	if err != nil {
		c.logger.Error("Failed to get Discord server count",
			zap.Int64("robloxUserID", robloxUserID),
			zap.Error(err))

		return false, 0, 0.0
	}

	// Check if user meets threshold (3+ servers)
	if count >= 3 {
		confidence = c.calculateDiscordConfidence(count)

		return true, count, confidence
	}

	return false, count, 0.0
}

// detectGroupActivity checks if a user should be flagged for condo group activity.
// Returns true if user meets criteria, along with count and details for logging.
func (c *CondoChecker) detectGroupActivity(
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

// calculateDiscordConfidence computes a confidence score based on Discord server count.
func (c *CondoChecker) calculateDiscordConfidence(serverCount int) float64 {
	switch {
	case serverCount >= 5:
		return 0.95
	case serverCount == 4:
		return 0.90
	case serverCount == 3:
		return 0.85
	default:
		return 0.0
	}
}

// calculateGroupConfidence computes a confidence score based on the number of condo groups.
func (c *CondoChecker) calculateGroupConfidence(condoCount int) float64 {
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

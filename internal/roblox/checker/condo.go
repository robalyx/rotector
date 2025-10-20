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
func (c *CondoChecker) ProcessUsers(ctx context.Context, params *CondoCheckerParams) error {
	existingFlags := len(params.ReasonsMap)

	// Track users flagged by each method
	discordFlaggedCount := 0
	groupsFlaggedCount := 0

	// Batch query Discord user IDs for all Roblox users
	robloxUserIDs := make([]int64, 0, len(params.Users))
	for _, userInfo := range params.Users {
		robloxUserIDs = append(robloxUserIDs, userInfo.ID)
	}

	robloxToDiscordMap, err := c.db.Model().Sync().GetDiscordUserIDsByRobloxIDs(ctx, robloxUserIDs)
	if err != nil {
		return fmt.Errorf("failed to get Discord user IDs for Roblox users: %w", err)
	}

	// Step 1: Check for Discord activity
	for _, userInfo := range params.Users {
		// Get Discord user ID for evidence
		discordUserID, hasDiscordConnection := robloxToDiscordMap[userInfo.ID]

		// Check Discord server count
		shouldFlag, serverCount, confidence := c.detectDiscordActivity(ctx, userInfo.ID)
		if shouldFlag {
			// User meets server threshold (3+), skip message analysis
			reasonMessage := fmt.Sprintf("Discord user with linked Roblox account detected in %d+ condo servers", serverCount)

			if _, exists := params.ReasonsMap[userInfo.ID]; !exists {
				params.ReasonsMap[userInfo.ID] = make(types.Reasons[enum.UserReasonType])
			}

			condoReason := &types.Reason{
				Message:    reasonMessage,
				Confidence: confidence,
			}

			// Add Discord User ID as evidence if available
			if hasDiscordConnection {
				condoReason.Evidence = []string{
					fmt.Sprintf("Discord User ID: %d", discordUserID),
				}
			}

			params.ReasonsMap[userInfo.ID].AddWithSource(enum.UserReasonTypeCondo, condoReason, "Discord")

			discordFlaggedCount++

			c.logger.Debug("User flagged for Discord condo server activity",
				zap.Int64("userID", userInfo.ID),
				zap.Int("serverCount", serverCount),
				zap.Float64("confidence", confidence))

			continue
		}

		// Server count below threshold, check for inappropriate messages
		shouldFlag, messageCount, confidence, reason := c.detectMessageActivity(ctx, userInfo.ID, robloxToDiscordMap)
		if shouldFlag {
			// Build reason message with server count
			reasonMessage := fmt.Sprintf(
				"Discord user with linked Roblox account detected in %d condo server(s) with inappropriate messages",
				serverCount,
			)

			if _, exists := params.ReasonsMap[userInfo.ID]; !exists {
				params.ReasonsMap[userInfo.ID] = make(types.Reasons[enum.UserReasonType])
			}

			condoReason := &types.Reason{
				Message:    reasonMessage,
				Confidence: confidence,
			}

			// Add Discord User ID as evidence if available
			if hasDiscordConnection {
				condoReason.Evidence = []string{
					fmt.Sprintf("Discord User ID: %d", discordUserID),
				}
			}

			params.ReasonsMap[userInfo.ID].AddWithSource(enum.UserReasonTypeCondo, condoReason, "Discord")

			discordFlaggedCount++

			c.logger.Debug("User flagged for inappropriate Discord messages",
				zap.Int64("userID", userInfo.ID),
				zap.Int("serverCount", serverCount),
				zap.Int("messageCount", messageCount),
				zap.Float64("confidence", confidence),
				zap.String("reason", reason))
		}
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

	return nil
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

// detectMessageActivity checks if a user should be flagged based on inappropriate Discord messages.
// Returns true if user has inappropriate messages, along with count, confidence, and reason.
func (c *CondoChecker) detectMessageActivity(
	ctx context.Context, robloxUserID int64, robloxToDiscordMap map[int64]uint64,
) (shouldFlag bool, messageCount int, confidence float64, reason string) {
	// Get Discord user ID for this Roblox user
	discordUserID, hasDiscordConnection := robloxToDiscordMap[robloxUserID]
	if !hasDiscordConnection {
		return false, 0, 0.0, ""
	}

	// Query message summary for this Discord user
	messageSummaries, err := c.db.Model().Message().GetUserInappropriateMessageSummaries(
		ctx, []uint64{discordUserID},
	)
	if err != nil {
		c.logger.Error("Failed to get message summaries",
			zap.Int64("robloxUserID", robloxUserID),
			zap.Uint64("discordUserID", discordUserID),
			zap.Error(err))

		return false, 0, 0.0, ""
	}

	// Check if this user has inappropriate messages
	summary, hasMessages := messageSummaries[discordUserID]
	if !hasMessages || summary.MessageCount == 0 {
		return false, 0, 0.0, ""
	}

	// Use fixed high confidence for AI-flagged messages
	confidence = 0.90

	return true, summary.MessageCount, confidence, summary.Reason
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

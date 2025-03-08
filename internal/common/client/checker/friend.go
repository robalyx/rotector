package checker

import (
	"context"
	"math"
	"time"

	"github.com/robalyx/rotector/internal/common/client/ai"
	"github.com/robalyx/rotector/internal/common/setup"
	"github.com/robalyx/rotector/internal/common/storage/database"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
	"go.uber.org/zap"
)

// FriendAnalysis contains the result of analyzing a user's friend network.
type FriendAnalysis struct {
	Name     string `json:"name"`
	Analysis string `json:"analysis"`
}

// FriendCheckResult contains the result of checking a user's friends.
type FriendCheckResult struct {
	UserID      uint64
	User        *types.User
	AutoFlagged bool
}

// FriendChecker handles the analysis of user friend relationships to identify
// users connected to multiple flagged accounts.
type FriendChecker struct {
	db             database.Client
	friendAnalyzer *ai.FriendAnalyzer
	logger         *zap.Logger
}

// NewFriendChecker creates a FriendChecker.
func NewFriendChecker(app *setup.App, logger *zap.Logger) *FriendChecker {
	return &FriendChecker{
		db:             app.DB,
		friendAnalyzer: ai.NewFriendAnalyzer(app, logger),
		logger:         logger.Named("friend_checker"),
	}
}

// ProcessUsers checks multiple users' friends concurrently and updates reasonsMap.
func (c *FriendChecker) ProcessUsers(userInfos []*types.User, reasonsMap map[uint64]types.Reasons[enum.UserReasonType]) {
	// Track counts before processing
	existingFlags := len(reasonsMap)

	// Collect all unique friend IDs across all users
	uniqueFriendIDs := make(map[uint64]struct{})
	for _, userInfo := range userInfos {
		for _, friend := range userInfo.Friends {
			uniqueFriendIDs[friend.ID] = struct{}{}
		}
	}

	// Convert unique IDs to slice
	friendIDs := make([]uint64, 0, len(uniqueFriendIDs))
	for friendID := range uniqueFriendIDs {
		friendIDs = append(friendIDs, friendID)
	}

	// Fetch all existing friends
	existingFriends, err := c.db.Models().Users().GetUsersByIDs(
		context.Background(), friendIDs, types.UserFieldBasic|types.UserFieldReasons,
	)
	if err != nil {
		c.logger.Error("Failed to fetch existing friends", zap.Error(err))
		return
	}

	// Prepare maps for confirmed and flagged friends per user
	confirmedFriendsMap := make(map[uint64]map[uint64]*types.User)
	flaggedFriendsMap := make(map[uint64]map[uint64]*types.User)

	for _, userInfo := range userInfos {
		confirmedFriends := make(map[uint64]*types.User)
		flaggedFriends := make(map[uint64]*types.User)

		for _, friend := range userInfo.Friends {
			if reviewUser, exists := existingFriends[friend.ID]; exists {
				switch reviewUser.Status {
				case enum.UserTypeConfirmed:
					confirmedFriends[friend.ID] = &reviewUser.User
				case enum.UserTypeFlagged:
					flaggedFriends[friend.ID] = &reviewUser.User
				} //exhaustive:ignore
			}
		}

		confirmedFriendsMap[userInfo.ID] = confirmedFriends
		flaggedFriendsMap[userInfo.ID] = flaggedFriends
	}

	// Generate reasons for all users
	reasons := c.friendAnalyzer.GenerateFriendReasons(context.Background(), userInfos, confirmedFriendsMap, flaggedFriendsMap)

	// Process results
	for _, userInfo := range userInfos {
		confirmedCount := len(confirmedFriendsMap[userInfo.ID])
		flaggedCount := len(flaggedFriendsMap[userInfo.ID])

		// Calculate confidence score
		confidence := c.calculateConfidence(confirmedCount, flaggedCount, len(userInfo.Friends))

		// Flag user if confidence exceeds threshold
		if confidence >= 0.50 {
			reason := reasons[userInfo.ID]
			if reason == "" {
				// Fallback to default reason format if AI generation failed
				reason = "User has flagged friends."
			}

			// Add new reason to reasons map
			if _, exists := reasonsMap[userInfo.ID]; !exists {
				reasonsMap[userInfo.ID] = make(types.Reasons[enum.UserReasonType])
			}
			reasonsMap[userInfo.ID].Add(enum.UserReasonTypeFriend, &types.Reason{
				Message:    reason,
				Confidence: confidence,
			})

			c.logger.Debug("User automatically flagged",
				zap.Uint64("userID", userInfo.ID),
				zap.Int("confirmedFriends", confirmedCount),
				zap.Int("flaggedFriends", flaggedCount),
				zap.Float64("confidence", confidence),
				zap.Int("accountAgeDays", int(time.Since(userInfo.CreatedAt).Hours()/24)),
				zap.String("reason", reason))
		}
	}

	c.logger.Info("Finished processing friends",
		zap.Int("totalUsers", len(userInfos)),
		zap.Int("newFlags", len(reasonsMap)-existingFlags))
}

// calculateConfidence computes a weighted confidence score based on friend relationships.
// The score considers both ratios and absolute numbers.
func (c *FriendChecker) calculateConfidence(confirmedCount, flaggedCount, totalFriends int) float64 {
	var confidence float64

	// Factor 1: Ratio of inappropriate friends - 50% weight
	// This helps catch users with a high concentration of inappropriate friends
	// even if they don't meet the absolute number thresholds
	if totalFriends > 0 {
		totalInappropriate := float64(confirmedCount) + (float64(flaggedCount) * 0.5)
		ratioWeight := math.Min(totalInappropriate/float64(totalFriends), 1.0)
		confidence += ratioWeight * 0.50
	}

	// Factor 2: Absolute number of inappropriate friends - 50% weight
	inappropriateWeight := c.calculateInappropriateWeight(confirmedCount, flaggedCount)
	confidence += inappropriateWeight * 0.50

	return confidence
}

// calculateInappropriateWeight returns a weight based on the total number of inappropriate friends.
// Confirmed friends are weighted more heavily than flagged friends.
func (c *FriendChecker) calculateInappropriateWeight(confirmedCount, flaggedCount int) float64 {
	totalWeight := float64(confirmedCount) + (float64(flaggedCount) * 0.5)

	switch {
	case confirmedCount >= 10 || totalWeight >= 15:
		return 1.0
	case confirmedCount >= 8 || totalWeight >= 12:
		return 0.8
	case confirmedCount >= 6 || totalWeight >= 9:
		return 0.6
	case confirmedCount >= 4 || totalWeight >= 6:
		return 0.4
	case confirmedCount >= 2 || totalWeight >= 3:
		return 0.2
	default:
		return 0.0
	}
}

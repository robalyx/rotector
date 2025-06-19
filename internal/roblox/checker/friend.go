package checker

import (
	"context"
	"math"
	"time"

	"github.com/robalyx/rotector/internal/ai"
	"github.com/robalyx/rotector/internal/database"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/database/types/enum"
	"github.com/robalyx/rotector/internal/setup"
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
	friendAnalyzer *ai.FriendReasonAnalyzer
	logger         *zap.Logger
}

// NewFriendChecker creates a FriendChecker.
func NewFriendChecker(app *setup.App, logger *zap.Logger) *FriendChecker {
	return &FriendChecker{
		db:             app.DB,
		friendAnalyzer: ai.NewFriendReasonAnalyzer(app, logger),
		logger:         logger.Named("friend_checker"),
	}
}

// ProcessUsers checks multiple users' friends concurrently and updates reasonsMap.
// Returns maps of confirmed and flagged friends for reuse by other checkers.
func (c *FriendChecker) ProcessUsers(
	ctx context.Context, userInfos []*types.ReviewUser, reasonsMap map[uint64]types.Reasons[enum.UserReasonType],
) (map[uint64]map[uint64]*types.ReviewUser, map[uint64]map[uint64]*types.ReviewUser) {
	existingFlags := len(reasonsMap)

	// Prepare friend maps
	confirmedFriendsMap, flaggedFriendsMap := c.PrepareFriendMaps(ctx, userInfos)

	// Track users that exceed confidence threshold
	var usersToAnalyze []*types.ReviewUser
	userConfidenceMap := make(map[uint64]float64)
	userFlaggedCountMap := make(map[uint64]int)

	// Process results
	for _, userInfo := range userInfos {
		confirmedCount := len(confirmedFriendsMap[userInfo.ID])
		flaggedCount := c.countValidFlaggedFriends(flaggedFriendsMap[userInfo.ID])
		userFlaggedCountMap[userInfo.ID] = flaggedCount

		// Calculate confidence score
		confidence := c.calculateConfidence(confirmedCount, flaggedCount, len(userInfo.Friends))
		userConfidenceMap[userInfo.ID] = confidence

		// Only process users that exceed threshold and have friends
		if confidence >= 0.50 && len(userInfo.Friends) > 0 {
			usersToAnalyze = append(usersToAnalyze, userInfo)
		}
	}

	// Generate AI reasons if we have users to analyze
	if len(usersToAnalyze) > 0 {
		// Generate reasons for flagged users
		reasons := c.friendAnalyzer.GenerateFriendReasons(ctx, usersToAnalyze, confirmedFriendsMap, flaggedFriendsMap)

		// Process results and update reasonsMap
		for _, userInfo := range usersToAnalyze {
			confirmedCount := len(confirmedFriendsMap[userInfo.ID])
			flaggedCount := userFlaggedCountMap[userInfo.ID]
			confidence := userConfidenceMap[userInfo.ID]

			reason := reasons[userInfo.ID]
			if reason == "" {
				// Fallback to default reason format if AI generation failed
				reason = "User has flagged friends in their friend network."
			}

			// Add new reason to reasons map
			if _, exists := reasonsMap[userInfo.ID]; !exists {
				reasonsMap[userInfo.ID] = make(types.Reasons[enum.UserReasonType])
			}
			reasonsMap[userInfo.ID].Add(enum.UserReasonTypeFriend, &types.Reason{
				Message:    reason,
				Confidence: confidence,
			})

			c.logger.Debug("User flagged for friend network",
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
		zap.Int("analyzedUsers", len(usersToAnalyze)),
		zap.Int("newFlags", len(reasonsMap)-existingFlags))

	return confirmedFriendsMap, flaggedFriendsMap
}

// PrepareFriendMaps creates friend maps for reuse by checkers.
// This method extracts the friend map preparation logic for reusability.
func (c *FriendChecker) PrepareFriendMaps(
	ctx context.Context, userInfos []*types.ReviewUser,
) (map[uint64]map[uint64]*types.ReviewUser, map[uint64]map[uint64]*types.ReviewUser) {
	confirmedFriendsMap := make(map[uint64]map[uint64]*types.ReviewUser)
	flaggedFriendsMap := make(map[uint64]map[uint64]*types.ReviewUser)

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

	// If no friends, return empty maps
	if len(friendIDs) == 0 {
		for _, userInfo := range userInfos {
			confirmedFriendsMap[userInfo.ID] = make(map[uint64]*types.ReviewUser)
			flaggedFriendsMap[userInfo.ID] = make(map[uint64]*types.ReviewUser)
		}
		return confirmedFriendsMap, flaggedFriendsMap
	}

	// Fetch all existing friends
	existingFriends, err := c.db.Model().User().GetUsersByIDs(
		ctx, friendIDs, types.UserFieldBasic|types.UserFieldReasons,
	)
	if err != nil {
		c.logger.Error("Failed to fetch existing friends", zap.Error(err))
		// Return empty maps on error
		for _, userInfo := range userInfos {
			confirmedFriendsMap[userInfo.ID] = make(map[uint64]*types.ReviewUser)
			flaggedFriendsMap[userInfo.ID] = make(map[uint64]*types.ReviewUser)
		}
		return confirmedFriendsMap, flaggedFriendsMap
	}

	// Prepare maps for confirmed and flagged friends per user
	for _, userInfo := range userInfos {
		confirmedFriends := make(map[uint64]*types.ReviewUser)
		flaggedFriends := make(map[uint64]*types.ReviewUser)

		for _, friend := range userInfo.Friends {
			if reviewUser, exists := existingFriends[friend.ID]; exists {
				switch reviewUser.Status {
				case enum.UserTypeConfirmed:
					confirmedFriends[friend.ID] = reviewUser
				case enum.UserTypeFlagged:
					flaggedFriends[friend.ID] = reviewUser
				default:
					continue
				}
			}
		}

		confirmedFriendsMap[userInfo.ID] = confirmedFriends
		flaggedFriendsMap[userInfo.ID] = flaggedFriends
	}

	return confirmedFriendsMap, flaggedFriendsMap
}

// calculateConfidence computes a weighted confidence score based on friend relationships.
func (c *FriendChecker) calculateConfidence(confirmedCount, flaggedCount, totalFriends int) float64 {
	totalWeight := float64(confirmedCount) + (float64(flaggedCount) * 0.8)

	// Hard thresholds for serious cases
	if confirmedCount >= 10 || totalWeight >= 20 {
		return 1.0
	}

	// Absolute count bonuses to prevent gaming large networks
	var absoluteBonus float64
	switch {
	case confirmedCount >= 20:
		absoluteBonus = 0.3
	case confirmedCount >= 12:
		absoluteBonus = 0.15
	case confirmedCount >= 8:
		absoluteBonus = 0.05
	}

	// Calculate thresholds based on network size
	var adjustedThreshold float64
	switch {
	case totalFriends >= 500:
		adjustedThreshold = math.Max(22.0, 0.035*float64(totalFriends))
	case totalFriends >= 200:
		adjustedThreshold = math.Max(16.0, 0.06*float64(totalFriends))
	case totalFriends >= 50:
		adjustedThreshold = math.Max(5.0, 0.085*float64(totalFriends))
	case totalFriends >= 25:
		adjustedThreshold = math.Max(3.0, 0.11*float64(totalFriends))
	default:
		adjustedThreshold = math.Max(2.0, 0.14*float64(totalFriends))
	}

	weightedThreshold := adjustedThreshold * 1.2
	confirmedRatio := float64(confirmedCount) / adjustedThreshold
	weightedRatio := totalWeight / weightedThreshold
	maxRatio := math.Max(confirmedRatio, weightedRatio)

	var baseConfidence float64
	switch {
	case maxRatio >= 2.0:
		baseConfidence = 1.0
	case maxRatio >= 1.5:
		baseConfidence = 0.8
	case maxRatio >= 1.0:
		baseConfidence = 0.6
	case maxRatio >= 0.7:
		baseConfidence = 0.4
	case maxRatio >= 0.5:
		baseConfidence = 0.3
	case maxRatio >= 0.3:
		baseConfidence = 0.2
	default:
		baseConfidence = 0.0
	}

	return math.Min(baseConfidence+absoluteBonus, 1.0)
}

// countValidFlaggedFriends counts flagged friends, excluding those who only have
// friend reason as their only reason to avoid false positives from circular flagging.
func (c *FriendChecker) countValidFlaggedFriends(flaggedFriends map[uint64]*types.ReviewUser) int {
	count := 0
	for _, flaggedFriend := range flaggedFriends {
		if len(flaggedFriend.Reasons) == 1 {
			if _, hasOnlyFriendReason := flaggedFriend.Reasons[enum.UserReasonTypeFriend]; hasOnlyFriendReason {
				continue
			}
		}
		count++
	}
	return count
}

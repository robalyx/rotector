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
	existingFriends, err := c.db.Model().User().GetUsersByIDs(
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
					confirmedFriends[friend.ID] = reviewUser.User
				case enum.UserTypeFlagged:
					flaggedFriends[friend.ID] = reviewUser.User
				} //exhaustive:ignore
			}
		}

		confirmedFriendsMap[userInfo.ID] = confirmedFriends
		flaggedFriendsMap[userInfo.ID] = flaggedFriends
	}

	// Track users that exceed confidence threshold
	var usersToAnalyze []*types.User

	// Process results
	for _, userInfo := range userInfos {
		confirmedCount := len(confirmedFriendsMap[userInfo.ID])
		flaggedCount := len(flaggedFriendsMap[userInfo.ID])

		// Calculate confidence score
		confidence := c.calculateConfidence(confirmedCount, flaggedCount, len(userInfo.Friends))

		// Only process users that exceed threshold
		if confidence >= 0.50 {
			usersToAnalyze = append(usersToAnalyze, userInfo)
		}
	}

	// Generate AI reasons if we have users to analyze
	if len(usersToAnalyze) > 0 {
		// Generate reasons for flagged users
		reasons := c.friendAnalyzer.GenerateFriendReasons(usersToAnalyze, confirmedFriendsMap, flaggedFriendsMap)

		// Process results and update reasonsMap
		for _, userInfo := range usersToAnalyze {
			confirmedCount := len(confirmedFriendsMap[userInfo.ID])
			flaggedCount := len(flaggedFriendsMap[userInfo.ID])
			confidence := c.calculateConfidence(confirmedCount, flaggedCount, len(userInfo.Friends))

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
}

// calculateConfidence computes a weighted confidence score based on friend relationships.
func (c *FriendChecker) calculateConfidence(confirmedCount, flaggedCount, totalFriends int) float64 {
	var confidence float64

	// Adjust weights based on total friend count
	// For users with large friend lists, ratio becomes more important
	ratioWeight := 0.50
	absoluteWeight := 0.50
	if totalFriends > 200 {
		ratioWeight = 0.60
		absoluteWeight = 0.40
	}

	// Factor 1: Ratio of inappropriate friends
	if totalFriends > 0 {
		totalInappropriate := float64(confirmedCount) + (float64(flaggedCount) * 0.5)
		ratioFactor := math.Min(totalInappropriate/float64(totalFriends), 1.0)
		confidence += ratioFactor * ratioWeight
	}

	// Factor 2: Absolute number of inappropriate friends
	inappropriateWeight := c.calculateInappropriateWeight(confirmedCount, flaggedCount, totalFriends)
	confidence += inappropriateWeight * absoluteWeight

	return confidence
}

// calculateInappropriateWeight returns a weight based on the total number of inappropriate friends.
func (c *FriendChecker) calculateInappropriateWeight(confirmedCount, flaggedCount, totalFriends int) float64 {
	totalWeight := float64(confirmedCount) + (float64(flaggedCount) * 0.5)

	// Calculate percentage thresholds based on friend count
	var baseThreshold float64
	switch {
	case totalFriends >= 500: // Large networks
		baseThreshold = 0.02 // 2% base threshold
	case totalFriends >= 200: // Medium networks
		baseThreshold = 0.03 // 3% base threshold
	default: // Small networks
		baseThreshold = 0.04 // 4% base threshol
	}

	// Calculate actual thresholds
	minConfirmed := 3
	minWeighted := 5

	thresholdConfirmed := math.Max(float64(minConfirmed), baseThreshold*float64(totalFriends))
	thresholdWeighted := math.Max(float64(minWeighted), (baseThreshold*1.5)*float64(totalFriends))

	// Hard threshold for serious cases
	if confirmedCount >= 15 || totalWeight >= 25 {
		return 1.0
	}

	// Determine confidence based on percentage thresholds
	switch {
	case float64(confirmedCount) >= thresholdConfirmed*1.5 || totalWeight >= thresholdWeighted*1.5:
		return 1.0
	case float64(confirmedCount) >= thresholdConfirmed*1.2 || totalWeight >= thresholdWeighted*1.2:
		return 0.8
	case float64(confirmedCount) >= thresholdConfirmed || totalWeight >= thresholdWeighted:
		return 0.6
	case float64(confirmedCount) >= thresholdConfirmed*0.7 || totalWeight >= thresholdWeighted*0.7:
		return 0.4
	case float64(confirmedCount) >= thresholdConfirmed*0.4 || totalWeight >= thresholdWeighted*0.4:
		return 0.2
	default:
		return 0.0
	}
}

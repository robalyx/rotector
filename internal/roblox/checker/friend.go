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

// FriendCheckerParams contains all the parameters needed for friend checker processing.
type FriendCheckerParams struct {
	Users                     []*types.ReviewUser                           `json:"users"`
	ReasonsMap                map[uint64]types.Reasons[enum.UserReasonType] `json:"reasonsMap"`
	ConfirmedFriendsMap       map[uint64]map[uint64]*types.ReviewUser       `json:"confirmedFriendsMap"`
	FlaggedFriendsMap         map[uint64]map[uint64]*types.ReviewUser       `json:"flaggedFriendsMap"`
	ConfirmedGroupsMap        map[uint64]map[uint64]*types.ReviewGroup      `json:"confirmedGroupsMap"`
	FlaggedGroupsMap          map[uint64]map[uint64]*types.ReviewGroup      `json:"flaggedGroupsMap"`
	InappropriateFriendsFlags map[uint64]struct{}                           `json:"inappropriateFriendsFlags"`
}

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
func (c *FriendChecker) ProcessUsers(ctx context.Context, params *FriendCheckerParams) {
	existingFlags := len(params.ReasonsMap)

	// Track users that exceed confidence threshold
	var usersToAnalyze []*types.ReviewUser

	userConfidenceMap := make(map[uint64]float64)
	userFlaggedCountMap := make(map[uint64]int)

	// Process results
	for _, userInfo := range params.Users {
		confirmedCount := len(params.ConfirmedFriendsMap[userInfo.ID])
		flaggedCount := len(params.FlaggedFriendsMap[userInfo.ID])
		validFlaggedCount := c.countValidFlaggedFriends(params.FlaggedFriendsMap[userInfo.ID])
		userFlaggedCountMap[userInfo.ID] = validFlaggedCount

		// Calculate confidence score
		_, isInappropriateFriends := params.InappropriateFriendsFlags[userInfo.ID]
		confidence := c.calculateConfidence(confirmedCount, validFlaggedCount, len(userInfo.Friends), isInappropriateFriends)

		userConfidenceMap[userInfo.ID] = confidence

		// Auto-flag users where all friends are inappropriate and they meet criteria
		totalFriends := len(userInfo.Friends)
		hasDescription := userInfo.Description != ""
		hasInappropriateGroups := c.hasInappropriateGroupActivity(userInfo, params.ConfirmedGroupsMap[userInfo.ID], params.FlaggedGroupsMap[userInfo.ID])
		hasExistingReasons := len(params.ReasonsMap[userInfo.ID]) > 0

		if confirmedCount+flaggedCount == totalFriends && totalFriends >= 2 &&
			(hasDescription || hasInappropriateGroups || hasExistingReasons) {
			// Auto-flag with maximum confidence
			confidence = 1.0
			userConfidenceMap[userInfo.ID] = confidence
			usersToAnalyze = append(usersToAnalyze, userInfo)
		} else {
			// Determine threshold based on whether user is in inappropriate friends map
			threshold := 0.50
			if _, isInappropriateFriends := params.InappropriateFriendsFlags[userInfo.ID]; isInappropriateFriends {
				threshold = 0.40
			}

			// Only process users that exceed threshold and have friends
			if confidence >= threshold && len(userInfo.Friends) > 0 {
				usersToAnalyze = append(usersToAnalyze, userInfo)
			}
		}
	}

	// Generate AI reasons if we have users to analyze
	if len(usersToAnalyze) > 0 {
		// Generate reasons for flagged users
		reasons := c.friendAnalyzer.GenerateFriendReasons(ctx, usersToAnalyze, params.ConfirmedFriendsMap, params.FlaggedFriendsMap)

		// Process results and update reasonsMap
		for _, userInfo := range usersToAnalyze {
			confirmedCount := len(params.ConfirmedFriendsMap[userInfo.ID])
			flaggedCount := userFlaggedCountMap[userInfo.ID]
			confidence := userConfidenceMap[userInfo.ID]

			reason := reasons[userInfo.ID]
			if reason == "" {
				// Fallback to default reason format if AI generation failed
				reason = "User has flagged friends in their friend network."
			}

			// Add new reason to reasons map
			if _, exists := params.ReasonsMap[userInfo.ID]; !exists {
				params.ReasonsMap[userInfo.ID] = make(types.Reasons[enum.UserReasonType])
			}

			params.ReasonsMap[userInfo.ID].Add(enum.UserReasonTypeFriend, &types.Reason{
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
		zap.Int("totalUsers", len(params.Users)),
		zap.Int("analyzedUsers", len(usersToAnalyze)),
		zap.Int("newFlags", len(params.ReasonsMap)-existingFlags))
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
func (c *FriendChecker) calculateConfidence(confirmedCount, flaggedCount, totalFriends int, enhanced bool) float64 {
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
	case totalFriends >= 800:
		adjustedThreshold = math.Max(25.0, 0.03*float64(totalFriends))
	case totalFriends >= 600:
		adjustedThreshold = math.Max(20.0, 0.032*float64(totalFriends))
	case totalFriends >= 500:
		adjustedThreshold = math.Max(18.0, 0.035*float64(totalFriends))
	case totalFriends >= 400:
		adjustedThreshold = math.Max(16.0, 0.04*float64(totalFriends))
	case totalFriends >= 300:
		adjustedThreshold = math.Max(14.0, 0.045*float64(totalFriends))
	case totalFriends >= 200:
		adjustedThreshold = math.Max(12.0, 0.055*float64(totalFriends))
	case totalFriends >= 150:
		adjustedThreshold = math.Max(9.0, 0.065*float64(totalFriends))
	case totalFriends >= 100:
		adjustedThreshold = math.Max(7.0, 0.075*float64(totalFriends))
	case totalFriends >= 75:
		adjustedThreshold = math.Max(6.0, 0.08*float64(totalFriends))
	case totalFriends >= 50:
		adjustedThreshold = math.Max(5.5, 0.08*float64(totalFriends))
	case totalFriends >= 35:
		adjustedThreshold = math.Max(5.0, 0.085*float64(totalFriends))
	case totalFriends >= 25:
		adjustedThreshold = math.Max(4.5, 0.09*float64(totalFriends))
	case totalFriends >= 15:
		adjustedThreshold = math.Max(4.0, 0.10*float64(totalFriends))
	default:
		adjustedThreshold = math.Max(2.5, 0.13*float64(totalFriends))
	}

	weightedThreshold := adjustedThreshold * 1.3
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

	finalConfidence := math.Min(baseConfidence+absoluteBonus, 1.0)

	if enhanced {
		finalConfidence = math.Min(finalConfidence*1.2, 1.0)
	}

	return finalConfidence
}

// hasInappropriateGroupActivity checks if at least half of the user's groups are flagged/confirmed.
func (c *FriendChecker) hasInappropriateGroupActivity(
	userInfo *types.ReviewUser, confirmedGroups, flaggedGroups map[uint64]*types.ReviewGroup,
) bool {
	totalGroups := len(userInfo.Groups)
	if totalGroups == 0 {
		return false
	}

	inappropriateCount := len(confirmedGroups) + len(flaggedGroups)

	// Check if at least half are inappropriate
	return float64(inappropriateCount) >= float64(totalGroups)/2.0
}

// countValidFlaggedFriends counts flagged friends, excluding those who only have
// friend reason or only friend + outfit reasons to avoid false positives from circular flagging.
func (c *FriendChecker) countValidFlaggedFriends(flaggedFriends map[uint64]*types.ReviewUser) int {
	count := 0

	for _, flaggedFriend := range flaggedFriends {
		// Skip users with only friend reason
		if len(flaggedFriend.Reasons) == 1 {
			if _, hasOnlyFriendReason := flaggedFriend.Reasons[enum.UserReasonTypeFriend]; hasOnlyFriendReason {
				continue
			}
		}

		// Skip users with only friend and outfit reasons
		if len(flaggedFriend.Reasons) == 2 {
			_, hasFriendReason := flaggedFriend.Reasons[enum.UserReasonTypeFriend]

			_, hasOutfitReason := flaggedFriend.Reasons[enum.UserReasonTypeOutfit]
			if hasFriendReason && hasOutfitReason {
				continue
			}
		}

		count++
	}

	return count
}

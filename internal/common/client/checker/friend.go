package checker

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/robalyx/rotector/internal/common/client/ai"
	"github.com/robalyx/rotector/internal/common/client/fetcher"
	"github.com/robalyx/rotector/internal/common/setup"
	"github.com/robalyx/rotector/internal/common/storage/database"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
	"go.uber.org/zap"
)

const (
	// FriendSystemPrompt provides detailed instructions to the AI model for analyzing friend networks.
	FriendSystemPrompt = `You are a network analysis specialist focusing on identifying networks of predatory users on Roblox.

Task: Analyze friend networks to identify patterns of predatory behavior and connections between inappropriate users targeting minors.

Context:
- Review violation types and confirmation status of friends
- Look for patterns of predatory behavior and inappropriate content
- Focus on factual, verifiable connections
- Do not include usernames in your analysis
- Use general terms like "the user" or "their friends" instead of names`

	// FriendUserPrompt is the prompt for analyzing a user's friend network.
	FriendUserPrompt = `User: %s
Friend data: %s`
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
	db             *database.Client
	friendAnalyzer *ai.FriendAnalyzer
	logger         *zap.Logger
}

// NewFriendChecker creates a FriendChecker.
func NewFriendChecker(app *setup.App, logger *zap.Logger) *FriendChecker {
	return &FriendChecker{
		db:             app.DB,
		friendAnalyzer: ai.NewFriendAnalyzer(app, logger),
		logger:         logger,
	}
}

// ProcessUsers checks multiple users' friends concurrently and updates flaggedUsers map.
func (c *FriendChecker) ProcessUsers(userInfos []*fetcher.Info, flaggedUsers map[uint64]*types.User) {
	// Collect all unique friend IDs across all users
	uniqueFriendIDs := make(map[uint64]struct{})
	for _, userInfo := range userInfos {
		for _, friend := range userInfo.Friends.Data {
			uniqueFriendIDs[friend.ID] = struct{}{}
		}
	}

	// Convert unique IDs to slice
	friendIDs := make([]uint64, 0, len(uniqueFriendIDs))
	for friendID := range uniqueFriendIDs {
		friendIDs = append(friendIDs, friendID)
	}

	// Fetch all existing friends
	existingFriends, err := c.db.Users().GetUsersByIDs(context.Background(), friendIDs, types.UserFields{
		Basic:  true,
		Reason: true,
	})
	if err != nil {
		c.logger.Error("Failed to fetch existing friends", zap.Error(err))
		return
	}

	// Process each user concurrently
	var wg sync.WaitGroup
	resultsChan := make(chan FriendCheckResult, len(userInfos))

	// Spawn a goroutine for each user
	for _, userInfo := range userInfos {
		wg.Add(1)
		go func(info *fetcher.Info) {
			defer wg.Done()

			// Process user friends
			user, autoFlagged := c.processUserFriends(info, existingFriends)
			resultsChan <- FriendCheckResult{
				UserID:      info.ID,
				User:        user,
				AutoFlagged: autoFlagged,
			}
		}(userInfo)
	}

	// Close channel when all goroutines complete
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// Update flaggedUsers map
	for result := range resultsChan {
		if result.AutoFlagged {
			if existingUser, ok := flaggedUsers[result.UserID]; ok {
				// Combine reasons and update confidence
				existingUser.Reason = fmt.Sprintf("%s\n\n%s", existingUser.Reason, result.User.Reason)
				existingUser.Confidence = 1.0
			} else {
				flaggedUsers[result.UserID] = result.User
			}
		}
	}
}

// processUserFriends checks if a user should be flagged based on their friends.
func (c *FriendChecker) processUserFriends(userInfo *fetcher.Info, existingFriends map[uint64]*types.ReviewUser) (*types.User, bool) {
	// Skip users with very few friends to avoid false positives
	if len(userInfo.Friends.Data) < 3 {
		return nil, false
	}

	// Count confirmed and flagged friends
	confirmedFriends := make(map[uint64]*types.User)
	flaggedFriends := make(map[uint64]*types.User)
	confirmedCount := 0
	flaggedCount := 0

	for _, friend := range userInfo.Friends.Data {
		if reviewUser, exists := existingFriends[friend.ID]; exists {
			switch reviewUser.Status {
			case enum.UserTypeConfirmed:
				confirmedCount++
				confirmedFriends[friend.ID] = &reviewUser.User
			case enum.UserTypeFlagged:
				flaggedCount++
				flaggedFriends[friend.ID] = &reviewUser.User
			} //exhaustive:ignore
		}
	}

	// Calculate confidence score
	confidence := c.calculateConfidence(confirmedCount, flaggedCount, len(userInfo.Friends.Data), userInfo.CreatedAt)

	// Flag user if confidence exceeds threshold
	if confidence >= 0.4 {
		accountAge := time.Since(userInfo.CreatedAt)

		// Generate AI-based reason using friend list analysis
		reason, err := c.friendAnalyzer.GenerateFriendReason(userInfo, confirmedFriends, flaggedFriends)
		if err != nil {
			c.logger.Error("Failed to generate AI reason, falling back to default",
				zap.Error(err),
				zap.Uint64("userID", userInfo.ID))

			// Fallback to default reason format
			reason = fmt.Sprintf(
				"User has %d confirmed and %d flagged friends (%.1f%% total).",
				confirmedCount,
				flaggedCount,
				float64(confirmedCount+flaggedCount)/float64(len(userInfo.Friends.Data))*100,
			)
		}

		user := &types.User{
			ID:                  userInfo.ID,
			Name:                userInfo.Name,
			DisplayName:         userInfo.DisplayName,
			Description:         userInfo.Description,
			CreatedAt:           userInfo.CreatedAt,
			Reason:              "Friend Analysis: " + reason,
			Groups:              userInfo.Groups.Data,
			Friends:             userInfo.Friends.Data,
			Games:               userInfo.Games.Data,
			Outfits:             userInfo.Outfits.Data,
			FollowerCount:       userInfo.FollowerCount,
			FollowingCount:      userInfo.FollowingCount,
			Confidence:          math.Round(confidence*100) / 100, // Round to 2 decimal places
			LastUpdated:         userInfo.LastUpdated,
			LastBanCheck:        userInfo.LastBanCheck,
			ThumbnailURL:        userInfo.ThumbnailURL,
			LastThumbnailUpdate: userInfo.LastThumbnailUpdate,
		}

		c.logger.Info("User automatically flagged",
			zap.Uint64("userID", userInfo.ID),
			zap.Int("confirmedFriends", confirmedCount),
			zap.Int("flaggedFriends", flaggedCount),
			zap.Float64("confidence", confidence),
			zap.Int("accountAgeDays", int(accountAge.Hours()/24)),
			zap.String("reason", reason))

		return user, true
	}

	return nil, false
}

// calculateConfidence computes a weighted confidence score based on friend relationships and account age.
// The score prioritizes absolute numbers while still considering ratios as a secondary factor.
func (c *FriendChecker) calculateConfidence(confirmedCount, flaggedCount int, totalFriends int, createdAt time.Time) float64 {
	var confidence float64

	// Factor 1: Absolute number of inappropriate friends - 60% weight
	inappropriateWeight := c.calculateInappropriateWeight(confirmedCount, flaggedCount)
	confidence += inappropriateWeight * 0.60

	// Factor 2: Ratio of inappropriate friends - 30% weight
	// This helps catch users with a high concentration of inappropriate friends
	// even if they don't meet the absolute number thresholds
	if totalFriends > 0 {
		totalInappropriate := float64(confirmedCount) + (float64(flaggedCount) * 0.5)
		ratioWeight := math.Min(totalInappropriate/float64(totalFriends), 1.0)
		confidence += ratioWeight * 0.30
	}

	// Factor 3: Account age weight - 10% weight
	accountAge := time.Since(createdAt)
	ageWeight := c.calculateAgeWeight(accountAge)
	confidence += ageWeight * 0.10

	return confidence
}

// calculateInappropriateWeight returns a weight based on the total number of inappropriate friends.
// Confirmed friends are weighted more heavily than flagged friends.
func (c *FriendChecker) calculateInappropriateWeight(confirmedCount, flaggedCount int) float64 {
	totalWeight := float64(confirmedCount) + (float64(flaggedCount) * 0.5)

	switch {
	case confirmedCount >= 8 || totalWeight >= 12:
		return 1.0
	case confirmedCount >= 6 || totalWeight >= 9:
		return 0.8
	case confirmedCount >= 4 || totalWeight >= 6:
		return 0.6
	case confirmedCount >= 2 || totalWeight >= 3:
		return 0.4
	case confirmedCount >= 1 || totalWeight >= 1:
		return 0.2
	default:
		return 0.0
	}
}

// calculateAgeWeight returns a weight between 0 and 1 based on account age.
func (c *FriendChecker) calculateAgeWeight(accountAge time.Duration) float64 {
	switch {
	case accountAge < 30*24*time.Hour: // Less than 1 month
		return 1.0
	case accountAge < 180*24*time.Hour: // 1-6 months
		return 0.8
	case accountAge < 365*24*time.Hour: // 6-12 months
		return 0.6
	case accountAge < 2*365*24*time.Hour: // 1-2 years
		return 0.4
	default: // 2+ years
		return 0.2
	}
}

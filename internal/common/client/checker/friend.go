package checker

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/rotector/rotector/internal/common/client/fetcher"
	"github.com/rotector/rotector/internal/common/storage/database"
	"github.com/rotector/rotector/internal/common/storage/database/models"
	"go.uber.org/zap"
)

// FriendChecker handles the analysis of user friend relationships to identify
// users connected to multiple flagged accounts.
type FriendChecker struct {
	db        *database.Client
	aiChecker *AIChecker
	logger    *zap.Logger
}

// NewFriendChecker creates a FriendChecker.
func NewFriendChecker(db *database.Client, aiChecker *AIChecker, logger *zap.Logger) *FriendChecker {
	return &FriendChecker{
		db:        db,
		aiChecker: aiChecker,
		logger:    logger,
	}
}

// ProcessUsers checks multiple users' friends concurrently and returns flagged users.
func (fc *FriendChecker) ProcessUsers(userInfos []*fetcher.Info) []*models.User {
	// FriendCheckResult contains the result of checking a user's friends.
	type FriendCheckResult struct {
		UserID      uint64
		User        *models.User
		AutoFlagged bool
		Error       error
	}

	var wg sync.WaitGroup
	resultsChan := make(chan FriendCheckResult, len(userInfos))

	// Spawn a goroutine for each user
	for _, userInfo := range userInfos {
		wg.Add(1)
		go func(info *fetcher.Info) {
			defer wg.Done()

			// Process user friends
			user, autoFlagged, err := fc.processUserFriends(info)
			resultsChan <- FriendCheckResult{
				UserID:      info.ID,
				User:        user,
				AutoFlagged: autoFlagged,
				Error:       err,
			}
		}(userInfo)
	}

	// Close the channel when all goroutines are done
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// Collect user infos and results
	userInfoMap := make(map[uint64]*fetcher.Info)
	for _, info := range userInfos {
		userInfoMap[info.ID] = info
	}

	results := make(map[uint64]*FriendCheckResult)
	for result := range resultsChan {
		results[result.UserID] = &result
	}

	// Collect flagged users
	var flaggedUsers []*models.User

	for userID, result := range results {
		if result.Error != nil {
			fc.logger.Error("Error checking user friends",
				zap.Error(result.Error),
				zap.Uint64("userID", userID))
			continue
		}

		if result.AutoFlagged {
			flaggedUsers = append(flaggedUsers, result.User)
		}
	}

	return flaggedUsers
}

// processUserFriends checks if a user should be flagged based on their friends.
func (fc *FriendChecker) processUserFriends(userInfo *fetcher.Info) (*models.User, bool, error) {
	// Skip users with very few friends to avoid false positives
	if len(userInfo.Friends.Data) < 3 {
		return nil, false, nil
	}

	// Extract friend IDs
	friendIDs := make([]uint64, len(userInfo.Friends.Data))
	for i, friend := range userInfo.Friends.Data {
		friendIDs[i] = friend.ID
	}

	// Check which users already exist in the database
	existingUsers, userTypes, err := fc.db.Users().GetUsersByIDs(context.Background(), friendIDs)
	if err != nil {
		return nil, false, err
	}

	// Separate friends by type and count them
	confirmedFriends := make(map[uint64]*models.User)
	flaggedFriends := make(map[uint64]*models.User)
	confirmedCount := 0
	flaggedCount := 0

	for id, user := range existingUsers {
		switch userTypes[id] {
		case models.UserTypeConfirmed:
			confirmedCount++
			confirmedFriends[id] = user
		case models.UserTypeFlagged:
			flaggedCount++
			flaggedFriends[id] = user
		}
	}

	// Calculate confidence score
	confidence := fc.calculateConfidence(confirmedCount, flaggedCount, len(userInfo.Friends.Data), userInfo.CreatedAt)

	// Flag user if confidence exceeds threshold
	if confidence >= 0.4 {
		accountAge := time.Since(userInfo.CreatedAt)

		// Generate AI-based reason using friend list analysis
		reason, err := fc.aiChecker.GenerateFriendReason(userInfo, confirmedFriends, flaggedFriends)
		if err != nil {
			fc.logger.Error("Failed to generate AI reason, falling back to default",
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

		user := &models.User{
			ID:             userInfo.ID,
			Name:           userInfo.Name,
			DisplayName:    userInfo.DisplayName,
			Description:    userInfo.Description,
			CreatedAt:      userInfo.CreatedAt,
			Reason:         "Friend Analysis: " + reason,
			Groups:         userInfo.Groups.Data,
			Friends:        userInfo.Friends.Data,
			Games:          userInfo.Games.Data,
			FollowerCount:  userInfo.FollowerCount,
			FollowingCount: userInfo.FollowingCount,
			Confidence:     math.Round(confidence*100) / 100, // Round to 2 decimal places
			LastUpdated:    userInfo.LastUpdated,
		}

		fc.logger.Info("User automatically flagged",
			zap.Uint64("userID", userInfo.ID),
			zap.Int("confirmedFriends", confirmedCount),
			zap.Int("flaggedFriends", flaggedCount),
			zap.Float64("confidence", confidence),
			zap.Int("accountAgeDays", int(accountAge.Hours()/24)),
			zap.String("reason", reason))

		return user, true, nil
	}

	return nil, false, nil
}

// calculateConfidence computes a weighted confidence score based on friend relationships and account age.
// The score prioritizes absolute numbers while still considering ratios as a secondary factor.
func (fc *FriendChecker) calculateConfidence(confirmedCount, flaggedCount int, totalFriends int, createdAt time.Time) float64 {
	var confidence float64

	// Factor 1: Absolute number of inappropriate friends - 60% weight
	inappropriateWeight := fc.calculateInappropriateWeight(confirmedCount, flaggedCount)
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
	ageWeight := fc.calculateAgeWeight(accountAge)
	confidence += ageWeight * 0.10

	return confidence
}

// calculateInappropriateWeight returns a weight based on the total number of inappropriate friends.
// Confirmed friends are weighted more heavily than flagged friends.
func (fc *FriendChecker) calculateInappropriateWeight(confirmedCount, flaggedCount int) float64 {
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
func (fc *FriendChecker) calculateAgeWeight(accountAge time.Duration) float64 {
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

package checker

import (
	"fmt"
	"math"
	"sync"

	"github.com/rotector/rotector/internal/common/database"
	"github.com/rotector/rotector/internal/common/fetcher"
	"go.uber.org/zap"
)

// FriendChecker handles the analysis of user friend relationships to identify
// users connected to multiple flagged accounts.
type FriendChecker struct {
	db     *database.Database
	logger *zap.Logger
}

// FriendCheckResult contains the result of checking a user's friends.
type FriendCheckResult struct {
	User        *database.User
	AutoFlagged bool
	Error       error
}

// NewFriendChecker creates a FriendChecker.
func NewFriendChecker(db *database.Database, logger *zap.Logger) *FriendChecker {
	return &FriendChecker{
		db:     db,
		logger: logger,
	}
}

// ProcessUsers checks multiple users' friends concurrently and returns flagged users
// and remaining users that need further checking.
func (fc *FriendChecker) ProcessUsers(userInfos []*fetcher.Info) ([]*database.User, []*fetcher.Info) {
	var wg sync.WaitGroup
	resultsChan := make(chan struct {
		UserID uint64
		Result *FriendCheckResult
	}, len(userInfos))

	// Spawn a goroutine for each user
	for _, userInfo := range userInfos {
		wg.Add(1)
		go func(info *fetcher.Info) {
			defer wg.Done()

			// Process user friends
			user, autoFlagged, err := fc.processUserFriends(info)
			resultsChan <- struct {
				UserID uint64
				Result *FriendCheckResult
			}{
				UserID: info.ID,
				Result: &FriendCheckResult{
					User:        user,
					AutoFlagged: autoFlagged,
					Error:       err,
				},
			}
		}(userInfo)
	}

	// Close the channel when all goroutines are done
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// Create maps to track results and original userInfos
	results := make(map[uint64]*FriendCheckResult)
	userInfoMap := make(map[uint64]*fetcher.Info)
	for _, info := range userInfos {
		userInfoMap[info.ID] = info
	}

	// Collect results from the channel
	for result := range resultsChan {
		results[result.UserID] = result.Result
	}

	// Separate users into flagged and remaining
	var flaggedUsers []*database.User
	var remainingUsers []*fetcher.Info

	for userID, result := range results {
		if result.Error != nil {
			fc.logger.Error("Error checking user friends",
				zap.Error(result.Error),
				zap.Uint64("userID", userID))
			remainingUsers = append(remainingUsers, userInfoMap[userID])
			continue
		}

		if result.AutoFlagged {
			flaggedUsers = append(flaggedUsers, result.User)
		} else {
			remainingUsers = append(remainingUsers, userInfoMap[userID])
		}
	}

	return flaggedUsers, remainingUsers
}

// processUserFriends checks if a user should be flagged based on their friends.
func (fc *FriendChecker) processUserFriends(userInfo *fetcher.Info) (*database.User, bool, error) {
	if len(userInfo.Friends) < 3 {
		return nil, false, nil
	}

	// Extract friend IDs
	friendIDs := make([]uint64, len(userInfo.Friends))
	for i, friend := range userInfo.Friends {
		friendIDs[i] = friend.ID
	}

	// Check which users already exist in the database
	existingUsers, err := fc.db.Users().CheckExistingUsers(friendIDs)
	if err != nil {
		return nil, false, err
	}

	// Count flagged friends
	flaggedCount := 0
	for _, status := range existingUsers {
		if status == database.UserTypeConfirmed || status == database.UserTypeFlagged {
			flaggedCount++
		}
	}

	// If the user has 8 or more flagged friends, or 50% or more of their friends are flagged, flag the user
	flaggedRatio := float64(flaggedCount) / float64(len(userInfo.Friends))
	if flaggedCount >= 8 || flaggedRatio >= 0.5 {
		user := &database.User{
			ID:          userInfo.ID,
			Name:        userInfo.Name,
			DisplayName: userInfo.DisplayName,
			Description: userInfo.Description,
			CreatedAt:   userInfo.CreatedAt,
			Reason:      fmt.Sprintf("User has %d flagged friends (%.2f%%)", flaggedCount, flaggedRatio*100),
			Groups:      userInfo.Groups,
			Friends:     userInfo.Friends,
			Confidence:  math.Round(flaggedRatio*100) / 100, // Round to 2 decimal places
			LastUpdated: userInfo.LastUpdated,
		}

		fc.logger.Info("User automatically flagged",
			zap.Uint64("userID", userInfo.ID),
			zap.Int("flaggedFriends", flaggedCount),
			zap.Float64("flaggedRatio", flaggedRatio))

		return user, true, nil
	}

	return nil, false, nil
}

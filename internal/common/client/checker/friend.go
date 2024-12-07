package checker

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/bytedance/sonic"
	"github.com/google/generative-ai-go/genai"
	"github.com/rotector/rotector/internal/common/client/fetcher"
	"github.com/rotector/rotector/internal/common/setup"
	"github.com/rotector/rotector/internal/common/storage/database"
	"github.com/rotector/rotector/internal/common/storage/database/types"
	"github.com/tdewolff/minify/v2"
	"github.com/tdewolff/minify/v2/json"
	"go.uber.org/zap"
)

// FriendCheckResult contains the result of checking a user's friends.
type FriendCheckResult struct {
	UserID      uint64
	User        *types.User
	AutoFlagged bool
}

// FriendChecker handles the analysis of user friend relationships to identify
// users connected to multiple flagged accounts.
type FriendChecker struct {
	db       *database.Client
	genModel *genai.GenerativeModel
	minify   *minify.M
	logger   *zap.Logger
}

// NewFriendChecker creates a FriendChecker.
func NewFriendChecker(app *setup.App, logger *zap.Logger) *FriendChecker {
	// Create friend analysis model
	friendModel := app.GenAIClient.GenerativeModel(app.Config.Common.GeminiAI.Model)
	friendModel.SystemInstruction = genai.NewUserContent(genai.Text(FriendSystemPrompt))
	friendModel.GenerationConfig.ResponseMIMEType = "application/json"
	friendModel.GenerationConfig.ResponseSchema = &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"name": {
				Type:        genai.TypeString,
				Description: "Username being analyzed",
			},
			"reason": {
				Type:        genai.TypeString,
				Description: "Analysis of friend network patterns",
			},
			"confidence": {
				Type:        genai.TypeNumber,
				Description: "Confidence level in the analysis",
			},
		},
		Required: []string{"name", "reason", "confidence"},
	}
	friendTemp := float32(0.8)
	friendModel.Temperature = &friendTemp

	// Create a minifier for JSON optimization
	m := minify.New()
	m.AddFunc("application/json", json.Minify)

	return &FriendChecker{
		db:       app.DB,
		genModel: friendModel,
		minify:   m,
		logger:   logger,
	}
}

// ProcessUsers checks multiple users' friends concurrently and returns flagged users.
func (fc *FriendChecker) ProcessUsers(userInfos []*fetcher.Info) map[uint64]*types.User {
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
	existingFriends, friendTypes, err := fc.db.Users().GetUsersByIDs(context.Background(), friendIDs, types.UserFields{
		Basic:  true,
		Reason: true,
	})
	if err != nil {
		fc.logger.Error("Failed to fetch existing friends", zap.Error(err))
		return nil
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
			user, autoFlagged := fc.processUserFriends(info, existingFriends, friendTypes)
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

	// Collect flagged users
	flaggedUsers := make(map[uint64]*types.User)
	for result := range resultsChan {
		if result.AutoFlagged {
			flaggedUsers[result.UserID] = result.User
		}
	}

	return flaggedUsers
}

// processUserFriends checks if a user should be flagged based on their friends.
func (fc *FriendChecker) processUserFriends(userInfo *fetcher.Info, existingFriends map[uint64]*types.User, friendTypes map[uint64]types.UserType) (*types.User, bool) {
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
		if user, exists := existingFriends[friend.ID]; exists {
			switch friendTypes[friend.ID] {
			case types.UserTypeConfirmed:
				confirmedCount++
				confirmedFriends[friend.ID] = user
			case types.UserTypeFlagged:
				flaggedCount++
				flaggedFriends[friend.ID] = user
			} //exhaustive:ignore
		}
	}

	// Calculate confidence score
	confidence := fc.calculateConfidence(confirmedCount, flaggedCount, len(userInfo.Friends.Data), userInfo.CreatedAt)

	// Flag user if confidence exceeds threshold
	if confidence >= 0.4 {
		accountAge := time.Since(userInfo.CreatedAt)

		// Generate AI-based reason using friend list analysis
		reason, err := fc.generateFriendReason(userInfo, confirmedFriends, flaggedFriends)
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

		user := &types.User{
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

		return user, true
	}

	return nil, false
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

// generateFriendReason generates a friend network analysis reason using the Gemini model.
func (fc *FriendChecker) generateFriendReason(userInfo *fetcher.Info, confirmedFriends, flaggedFriends map[uint64]*types.User) (string, error) {
	// Create a summary of friend data for AI analysis
	type FriendSummary struct {
		Name   string         `json:"name"`
		Reason string         `json:"reason"`
		Type   types.UserType `json:"type"`
	}

	// Collect friend summaries with token counting
	friendSummaries := make([]FriendSummary, 0, len(confirmedFriends)+len(flaggedFriends))

	// Helper function to add friend if within token limit
	currentTokens := int32(0)
	addFriend := func(friend *types.User, friendType types.UserType) bool {
		summary := FriendSummary{
			Name:   friend.Name,
			Reason: friend.Reason,
			Type:   friendType,
		}

		// Convert to JSON to count tokens accurately
		summaryJSON, err := sonic.Marshal(summary)
		if err != nil {
			fc.logger.Warn("Failed to marshal friend summary",
				zap.String("username", friend.Name),
				zap.Error(err))
			return false
		}

		// Count and check token limit
		tokenCount, err := fc.genModel.CountTokens(context.Background(), genai.Text(summaryJSON))
		if err != nil {
			fc.logger.Warn("Failed to count tokens for friend summary",
				zap.String("username", friend.Name),
				zap.Error(err))
			return false
		}

		currentTokens += tokenCount.TotalTokens
		if currentTokens > MaxFriendDataTokens {
			return false
		}

		friendSummaries = append(friendSummaries, summary)
		return true
	}

	// Add confirmed friends first (they're usually more important)
	for _, friend := range confirmedFriends {
		if !addFriend(friend, types.UserTypeConfirmed) {
			break
		}
	}

	// Add flagged friends if there's room
	for _, friend := range flaggedFriends {
		if !addFriend(friend, types.UserTypeFlagged) {
			break
		}
	}

	// Convert to JSON for the AI request
	friendDataJSON, err := sonic.Marshal(friendSummaries)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrJSONProcessing, err)
	}

	// Minify JSON to reduce token usage
	friendDataJSON, err = fc.minify.Bytes("application/json", friendDataJSON)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrJSONProcessing, err)
	}

	// Configure prompt for friend analysis
	prompt := fmt.Sprintf(FriendUserPrompt, userInfo.Name, string(friendDataJSON))

	// Generate friend analysis using Gemini model
	resp, err := fc.genModel.GenerateContent(context.Background(), genai.Text(prompt))
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrModelResponse, err)
	}

	if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("%w: no response from Gemini", ErrModelResponse)
	}

	// Extract response text from Gemini's response
	responseText := resp.Candidates[0].Content.Parts[0].(genai.Text)

	// Parse Gemini response into FlaggedUser struct
	var flaggedUser FlaggedUser
	err = sonic.Unmarshal([]byte(responseText), &flaggedUser)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrJSONProcessing, err)
	}

	reason := flaggedUser.Reason
	fc.logger.Debug("Generated friend network reason",
		zap.String("username", userInfo.Name),
		zap.Int("confirmedFriends", len(confirmedFriends)),
		zap.Int("flaggedFriends", len(flaggedFriends)),
		zap.Int32("totalTokens", currentTokens),
		zap.String("generatedReason", reason))

	return reason, nil
}

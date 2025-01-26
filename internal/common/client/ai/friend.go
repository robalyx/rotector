package ai

import (
	"context"
	"fmt"

	"github.com/bytedance/sonic"
	"github.com/google/generative-ai-go/genai"
	"github.com/robalyx/rotector/internal/common/client/fetcher"
	"github.com/robalyx/rotector/internal/common/setup"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
	"github.com/robalyx/rotector/internal/common/utils"
	"github.com/tdewolff/minify/v2"
	"github.com/tdewolff/minify/v2/json"
	"go.uber.org/zap"
	"golang.org/x/sync/semaphore"
)

const (
	// FriendSystemPrompt provides detailed instructions to the AI model for analyzing friend networks.
	FriendSystemPrompt = `You are a network analysis specialist focusing on identifying networks of predatory users on Roblox.

Task: Analyze friend networks to identify patterns of predatory behavior and connections between inappropriate users targeting minors.

Instructions:
- Review violation types and confirmation status of friends
- Look for patterns of predatory behavior and inappropriate content
- Focus on factual, verifiable connections
- Keep your analysis concise and within 1 sentence
- Do not include usernames in your analysis`

	// FriendUserPrompt is the prompt for analyzing a user's friend network.
	FriendUserPrompt = `User: %s
Friend data: %s`
)

// FriendAnalysis contains the result of analyzing a user's friend network.
type FriendAnalysis struct {
	Name     string `json:"name"`
	Analysis string `json:"analysis"`
}

// FriendAnalyzer handles AI-based analysis of friend networks using Gemini models.
type FriendAnalyzer struct {
	genModel    *genai.GenerativeModel
	minify      *minify.M
	analysisSem *semaphore.Weighted
	logger      *zap.Logger
}

// NewFriendAnalyzer creates a FriendAnalyzer.
func NewFriendAnalyzer(app *setup.App, logger *zap.Logger) *FriendAnalyzer {
	// Create friend analysis model
	friendModel := app.GenAIClient.GenerativeModel(app.Config.Common.GeminiAI.Model)
	friendModel.SystemInstruction = genai.NewUserContent(genai.Text(FriendSystemPrompt))
	friendModel.ResponseMIMEType = ApplicationJSON
	friendModel.ResponseSchema = &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"name": {
				Type:        genai.TypeString,
				Description: "Username being analyzed",
			},
			"analysis": {
				Type:        genai.TypeString,
				Description: `Analysis of friend network patterns`,
			},
		},
		Required: []string{"name", "analysis"},
	}
	friendModel.Temperature = utils.Ptr(float32(0.2))
	friendModel.TopP = utils.Ptr(float32(0.4))
	friendModel.TopK = utils.Ptr(int32(8))

	// Create a minifier for JSON optimization
	m := minify.New()
	m.AddFunc(ApplicationJSON, json.Minify)

	return &FriendAnalyzer{
		genModel:    friendModel,
		minify:      m,
		analysisSem: semaphore.NewWeighted(int64(app.Config.Worker.BatchSizes.FriendAnalysis)),
		logger:      logger,
	}
}

// GenerateFriendReason generates a friend network analysis reason using the Gemini model.
func (a *FriendAnalyzer) GenerateFriendReason(userInfo *fetcher.Info, confirmedFriends, flaggedFriends map[uint64]*types.User) (string, error) {
	// Acquire semaphore before making AI request
	if err := a.analysisSem.Acquire(context.Background(), 1); err != nil {
		return "", fmt.Errorf("failed to acquire semaphore: %w", err)
	}
	defer a.analysisSem.Release(1)

	// Create a summary of friend data for AI analysis
	type FriendSummary struct {
		Name   string        `json:"name"`
		Reason string        `json:"reason"`
		Type   enum.UserType `json:"type"`
	}

	// Collect friend summaries with token counting
	friendSummaries := make([]FriendSummary, 0, len(confirmedFriends)+len(flaggedFriends))

	// Helper function to add friend if within token limit
	currentTokens := int32(0)
	addFriend := func(friend *types.User, friendType enum.UserType) bool {
		summary := FriendSummary{
			Name:   friend.Name,
			Reason: friend.Reason,
			Type:   friendType,
		}

		// Convert to JSON to count tokens accurately
		summaryJSON, err := sonic.Marshal(summary)
		if err != nil {
			a.logger.Warn("Failed to marshal friend summary",
				zap.String("username", friend.Name),
				zap.Error(err))
			return false
		}

		// Count and check token limit
		tokenCount, err := a.genModel.CountTokens(context.Background(), genai.Text(summaryJSON))
		if err != nil {
			a.logger.Warn("Failed to count tokens for friend summary",
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
		if !addFriend(friend, enum.UserTypeConfirmed) {
			break
		}
	}

	// Add flagged friends if there's room
	for _, friend := range flaggedFriends {
		if !addFriend(friend, enum.UserTypeFlagged) {
			break
		}
	}

	// Convert to JSON for the AI request
	friendDataJSON, err := sonic.Marshal(friendSummaries)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrJSONProcessing, err)
	}

	// Minify JSON to reduce token usage
	friendDataJSON, err = a.minify.Bytes(ApplicationJSON, friendDataJSON)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrJSONProcessing, err)
	}

	// Configure prompt for friend analysis
	prompt := fmt.Sprintf(FriendUserPrompt, userInfo.Name, string(friendDataJSON))

	// Generate friend analysis using Gemini model with retry
	friendAnalysis, err := withRetry(context.Background(), func() (*FriendAnalysis, error) {
		resp, err := a.genModel.GenerateContent(context.Background(), genai.Text(prompt))
		if err != nil {
			return nil, fmt.Errorf("gemini API error: %w", err)
		}

		if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
			return nil, fmt.Errorf("%w: no response from Gemini", ErrModelResponse)
		}

		// Extract and parse response
		responseText := resp.Candidates[0].Content.Parts[0].(genai.Text)
		var result FriendAnalysis
		if err := sonic.Unmarshal([]byte(responseText), &result); err != nil {
			return nil, fmt.Errorf("JSON unmarshal error: %w", err)
		}

		return &result, nil
	})
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrModelResponse, err)
	}

	reason := friendAnalysis.Analysis
	a.logger.Debug("Generated friend network reason",
		zap.String("username", userInfo.Name),
		zap.Int("confirmedFriends", len(confirmedFriends)),
		zap.Int("flaggedFriends", len(flaggedFriends)),
		zap.Int32("totalTokens", currentTokens),
		zap.String("generatedReason", reason))

	return reason, nil
}

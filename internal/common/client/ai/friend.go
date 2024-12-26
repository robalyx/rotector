package ai

import (
	"context"
	"fmt"

	"github.com/bytedance/sonic"
	"github.com/google/generative-ai-go/genai"
	"github.com/rotector/rotector/internal/common/client/fetcher"
	"github.com/rotector/rotector/internal/common/setup"
	"github.com/rotector/rotector/internal/common/storage/database/types"
	"github.com/tdewolff/minify/v2"
	"github.com/tdewolff/minify/v2/json"
	"go.uber.org/zap"
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
	genModel *genai.GenerativeModel
	minify   *minify.M
	logger   *zap.Logger
}

// NewFriendAnalyzer creates a FriendAnalyzer.
func NewFriendAnalyzer(app *setup.App, logger *zap.Logger) *FriendAnalyzer {
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
			"analysis": {
				Type:        genai.TypeString,
				Description: `Analysis of friend network patterns`,
			},
		},
		Required: []string{"name", "analysis"},
	}
	friendTemp := float32(0.2)
	friendModel.Temperature = &friendTemp

	// Create a minifier for JSON optimization
	m := minify.New()
	m.AddFunc("application/json", json.Minify)

	return &FriendAnalyzer{
		genModel: friendModel,
		minify:   m,
		logger:   logger,
	}
}

// GenerateFriendReason generates a friend network analysis reason using the Gemini model.
func (a *FriendAnalyzer) GenerateFriendReason(userInfo *fetcher.Info, confirmedFriends, flaggedFriends map[uint64]*types.User) (string, error) {
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
	friendDataJSON, err = a.minify.Bytes("application/json", friendDataJSON)
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

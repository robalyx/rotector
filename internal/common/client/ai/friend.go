package ai

import (
	"context"
	"fmt"
	"sync"

	"github.com/bytedance/sonic"
	"github.com/google/generative-ai-go/genai"
	"github.com/robalyx/rotector/internal/common/client/fetcher"
	"github.com/robalyx/rotector/internal/common/setup"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
	"github.com/robalyx/rotector/internal/common/utils"
	"github.com/sourcegraph/conc/pool"
	"github.com/tdewolff/minify/v2"
	"github.com/tdewolff/minify/v2/json"
	"go.uber.org/zap"
	"golang.org/x/sync/semaphore"
)

const (
	// FriendSystemPrompt provides detailed instructions to the AI model for analyzing friend networks.
	FriendSystemPrompt = `You are a network analyst identifying predatory behavior patterns in Roblox friend networks.

Input format:
{
  "username": "string",
  "friends": [
    {
      "name": "string",
      "type": "Confirmed|Flagged",
      "reasonTypes": ["user", "outfit", "group", "friend"]
    }
  ]
}

Output format:
{
  "results": [
    {
      "name": "string",
      "analysis": "Clear pattern summary in one sentence"
    }
  ]
}

Key rules:
1. Focus on factual connections
2. Use "the network" instead of usernames
3. Keep analysis to one sentence
4. Emphasize patterns across accounts
5. Return a result for each user
6. Consider accounts with few friends as potential alt accounts

Look for:
- Common violation types
- Confirmed vs flagged ratios
- Connected violation patterns
- Network size and density
- Violation clustering

Violation types:
- user: Profile content violations
- outfit: Inappropriate outfit designs
- group: Group-based violations
- friend: Network pattern violations`

	// FriendUserPrompt is the prompt for analyzing multiple users' friend networks.
	FriendUserPrompt = `Analyze these friend networks for predatory behavior patterns.

Remember:
1. Focus on factual connections
2. Use "the network" instead of usernames
3. Keep analysis to one sentence
4. Look for patterns across accounts
5. Return a result for each user

Networks to analyze:
%s`
)

const (
	// MaxFriends is the maximum number of friends to include in analysis.
	MaxFriends = 10
)

// FriendSummary contains a summary of a friend's data.
type FriendSummary struct {
	Name        string        `json:"name"`
	Type        enum.UserType `json:"type"`
	ReasonTypes []string      `json:"reasonTypes"`
}

// FriendAnalysis contains the result of analyzing a user's friend network.
type FriendAnalysis struct {
	Name     string `json:"name"`
	Analysis string `json:"analysis"`
}

// BatchFriendAnalysis contains results for multiple users' friend networks.
type BatchFriendAnalysis struct {
	Results []FriendAnalysis `json:"results"`
}

// FriendAnalyzer handles AI-based analysis of friend networks using Gemini models.
type FriendAnalyzer struct {
	genModel    *genai.GenerativeModel
	minify      *minify.M
	analysisSem *semaphore.Weighted
	batchSize   int
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
			"results": {
				Type: genai.TypeArray,
				Items: &genai.Schema{
					Type: genai.TypeObject,
					Properties: map[string]*genai.Schema{
						"name": {
							Type:        genai.TypeString,
							Description: "Username of the account being analyzed",
						},
						"analysis": {
							Type:        genai.TypeString,
							Description: "Analysis of friend network patterns for this user",
						},
					},
					Required: []string{"name", "analysis"},
				},
				Description: "Array of friend network analyses for each user",
			},
		},
		Required: []string{"results"},
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
		batchSize:   app.Config.Worker.BatchSizes.FriendAnalysisBatch,
		logger:      logger,
	}
}

// GenerateFriendReasons generates friend network analysis reasons for multiple users using the Gemini model.
func (a *FriendAnalyzer) GenerateFriendReasons(ctx context.Context, userInfos []*fetcher.Info, confirmedFriendsMap, flaggedFriendsMap map[uint64]map[uint64]*types.User) map[uint64]string {
	var (
		p       = pool.New().WithContext(ctx)
		mu      sync.Mutex
		results = make(map[uint64]string)
	)

	// Calculate number of batches
	numBatches := (len(userInfos) + a.batchSize - 1) / a.batchSize

	for i := range numBatches {
		start := i * a.batchSize
		end := start + a.batchSize
		if end > len(userInfos) {
			end = len(userInfos)
		}

		infoBatch := userInfos[start:end]
		p.Go(func(ctx context.Context) error {
			// Acquire semaphore before making AI request
			if err := a.analysisSem.Acquire(ctx, 1); err != nil {
				return fmt.Errorf("failed to acquire semaphore: %w", err)
			}
			defer a.analysisSem.Release(1)

			// Process batch
			batchResults, err := a.processBatch(ctx, infoBatch, confirmedFriendsMap, flaggedFriendsMap)
			if err != nil {
				a.logger.Error("Failed to process batch",
					zap.Error(err),
					zap.Int("batchStart", start),
					zap.Int("batchEnd", end))
				return err
			}

			// Store results
			mu.Lock()
			for _, result := range batchResults {
				for _, info := range infoBatch {
					if info.Name == result.Name {
						results[info.ID] = result.Analysis
						break
					}
				}
			}
			mu.Unlock()

			return nil
		})
	}

	if err := p.Wait(); err != nil {
		a.logger.Error("Error during friend reason generation", zap.Error(err))
	}

	return results
}

// processBatch handles analysis for a batch of users.
func (a *FriendAnalyzer) processBatch(ctx context.Context, userInfos []*fetcher.Info, confirmedFriendsMap, flaggedFriendsMap map[uint64]map[uint64]*types.User) ([]FriendAnalysis, error) {
	// Create summaries for all users in batch
	type UserFriendData struct {
		Username string          `json:"username"`
		Friends  []FriendSummary `json:"friends"`
	}

	batchData := make([]UserFriendData, 0, len(userInfos))

	for _, userInfo := range userInfos {
		// Get confirmed and flagged friends for this user
		confirmedFriends := confirmedFriendsMap[userInfo.ID]
		flaggedFriends := flaggedFriendsMap[userInfo.ID]

		// Collect friend summaries
		friendSummaries := make([]FriendSummary, 0, MaxFriends)

		// Add confirmed friends first
		for _, friend := range confirmedFriends {
			if len(friendSummaries) >= MaxFriends {
				break
			}
			friendSummaries = append(friendSummaries, FriendSummary{
				Name:        friend.Name,
				Type:        enum.UserTypeConfirmed,
				ReasonTypes: friend.Reasons.Types(),
			})
		}

		// Add flagged friends with remaining space
		for _, friend := range flaggedFriends {
			if len(friendSummaries) >= MaxFriends {
				break
			}
			friendSummaries = append(friendSummaries, FriendSummary{
				Name:        friend.Name,
				Type:        enum.UserTypeFlagged,
				ReasonTypes: friend.Reasons.Types(),
			})
		}

		batchData = append(batchData, UserFriendData{
			Username: userInfo.Name,
			Friends:  friendSummaries,
		})
	}

	// Skip if no users in batch
	if len(batchData) == 0 {
		return nil, nil
	}

	// Convert to JSON for the AI request
	batchDataJSON, err := sonic.Marshal(batchData)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrJSONProcessing, err)
	}

	// Minify JSON to reduce token usage
	batchDataJSON, err = a.minify.Bytes(ApplicationJSON, batchDataJSON)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrJSONProcessing, err)
	}

	// Configure prompt for friend analysis
	prompt := fmt.Sprintf(FriendUserPrompt, string(batchDataJSON))

	// Generate friend analysis using Gemini model with retry
	batchAnalysis, err := withRetry(ctx, func() (*BatchFriendAnalysis, error) {
		resp, err := a.genModel.GenerateContent(ctx, genai.Text(prompt))
		if err != nil {
			return nil, fmt.Errorf("gemini API error: %w", err)
		}

		if len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil || len(resp.Candidates[0].Content.Parts) == 0 {
			return nil, fmt.Errorf("%w: no response from Gemini", ErrModelResponse)
		}

		// Extract and parse response
		responseText := resp.Candidates[0].Content.Parts[0].(genai.Text)
		var result BatchFriendAnalysis
		if err := sonic.Unmarshal([]byte(responseText), &result); err != nil {
			return nil, fmt.Errorf("JSON unmarshal error: %w", err)
		}

		return &result, nil
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrModelResponse, err)
	}

	// Verify we got results for all users in the batch
	if len(batchAnalysis.Results) > 0 {
		// Create map of usernames we got results for
		resultUsers := make(map[string]struct{}, len(batchAnalysis.Results))
		for _, result := range batchAnalysis.Results {
			resultUsers[result.Name] = struct{}{}
		}

		// Check if we got results for all users we sent
		for _, data := range batchData {
			if _, ok := resultUsers[data.Username]; !ok {
				a.logger.Error("Missing friend analysis result",
					zap.String("username", data.Username))
			}
		}
	}

	return batchAnalysis.Results, nil
}

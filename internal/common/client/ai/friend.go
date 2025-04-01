package ai

import (
	"context"
	"fmt"
	"sync"

	"github.com/bytedance/sonic"
	"github.com/openai/openai-go"
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
	Name     string `json:"name"     jsonschema_description:"Username of the account being analyzed"`
	Analysis string `json:"analysis" jsonschema_description:"Analysis of friend network patterns for this user"`
}

// BatchFriendAnalysis contains results for multiple users' friend networks.
type BatchFriendAnalysis struct {
	Results []FriendAnalysis `json:"results" jsonschema_description:"Array of friend network analyses for each user"`
}

// FriendAnalyzer handles AI-based analysis of friend networks using OpenAI models.
type FriendAnalyzer struct {
	openAIClient *openai.Client
	minify       *minify.M
	analysisSem  *semaphore.Weighted
	logger       *zap.Logger
	model        string
	batchSize    int
}

// FriendAnalysisSchema is the JSON schema for the friend analysis response.
var FriendAnalysisSchema = utils.GenerateSchema[BatchFriendAnalysis]()

// NewFriendAnalyzer creates a FriendAnalyzer.
func NewFriendAnalyzer(app *setup.App, logger *zap.Logger) *FriendAnalyzer {
	// Create a minifier for JSON optimization
	m := minify.New()
	m.AddFunc(ApplicationJSON, json.Minify)

	return &FriendAnalyzer{
		openAIClient: app.OpenAIClient,
		minify:       m,
		analysisSem:  semaphore.NewWeighted(int64(app.Config.Worker.BatchSizes.FriendAnalysis)),
		logger:       logger.Named("ai_friend"),
		model:        app.Config.Common.OpenAI.Model,
		batchSize:    app.Config.Worker.BatchSizes.FriendAnalysisBatch,
	}
}

// GenerateFriendReasons generates friend network analysis reasons for multiple users using the OpenAI model.
func (a *FriendAnalyzer) GenerateFriendReasons(
	userInfos []*types.User, confirmedFriendsMap, flaggedFriendsMap map[uint64]map[uint64]*types.User,
) map[uint64]string {
	var (
		ctx     = context.Background()
		p       = pool.New().WithContext(ctx)
		mu      sync.Mutex
		results = make(map[uint64]string)
	)

	// Calculate number of batches
	numBatches := (len(userInfos) + a.batchSize - 1) / a.batchSize

	for i := range numBatches {
		start := i * a.batchSize
		end := min(start+a.batchSize, len(userInfos))

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
func (a *FriendAnalyzer) processBatch(
	ctx context.Context, userInfos []*types.User,
	confirmedFriendsMap, flaggedFriendsMap map[uint64]map[uint64]*types.User,
) ([]FriendAnalysis, error) {
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

	// Generate friend analysis
	resp, err := a.openAIClient.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(FriendSystemPrompt),
			openai.UserMessage(prompt),
		},
		ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONSchema: &openai.ResponseFormatJSONSchemaParam{
				JSONSchema: openai.ResponseFormatJSONSchemaJSONSchemaParam{
					Name:        "friendAnalysis",
					Description: openai.String("Analysis of friend network patterns"),
					Schema:      FriendAnalysisSchema,
					Strict:      openai.Bool(true),
				},
			},
		},
		Model:       a.model,
		Temperature: openai.Float(0.2),
		TopP:        openai.Float(0.4),
	})
	if err != nil {
		return nil, fmt.Errorf("openai API error: %w", err)
	}

	// Check for empty response
	if len(resp.Choices) == 0 || len(resp.Choices[0].Message.Content) == 0 {
		return nil, fmt.Errorf("%w: no response from model", ErrModelResponse)
	}

	// Parse response from AI
	var result *BatchFriendAnalysis
	if err := sonic.Unmarshal([]byte(resp.Choices[0].Message.Content), &result); err != nil {
		return nil, fmt.Errorf("JSON unmarshal error: %w", err)
	}

	// Verify we got results for all users in the batch
	if len(result.Results) > 0 {
		// Create map of usernames we got results for
		resultUsers := make(map[string]struct{}, len(result.Results))
		for _, result := range result.Results {
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

	return result.Results, nil
}

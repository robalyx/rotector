package ai

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/bytedance/sonic"
	"github.com/openai/openai-go"
	"github.com/robalyx/rotector/internal/ai/client"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/setup"
	"github.com/robalyx/rotector/pkg/utils"
	"github.com/sourcegraph/conc/pool"
	"github.com/tdewolff/minify/v2"
	"github.com/tdewolff/minify/v2/json"
	"go.uber.org/zap"
	"golang.org/x/sync/semaphore"
)

const (
	// FriendSystemPrompt provides detailed instructions to the AI model for analyzing friend networks.
	FriendSystemPrompt = `Instruction:
You are a network analyst identifying predatory behavior patterns in Roblox friend networks.

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

Key instructions:
1. Focus on factual connections
2. Use "the network" instead of usernames
3. Keep analysis to one sentence
4. Emphasize patterns across accounts
5. Return a result for each user
6. Consider accounts with few friends as potential alt accounts

Violation types:
- user: Profile content violations
- outfit: Inappropriate outfit designs
- group: Group-based violations
- friend: Network pattern violations

Instruction: Look for:
- Common violation types
- Confirmed vs flagged ratios
- Connected violation patterns
- Network size and density
- Violation clustering`

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
	Name        string   `json:"name"        jsonschema_description:"Username of the friend"`
	Type        string   `json:"type"        jsonschema_description:"Type of friend (Confirmed or Flagged)"`
	ReasonTypes []string `json:"reasonTypes" jsonschema_description:"List of reasons why this friend was flagged"`
}

// UserFriendData represents the data for a user's friend network.
type UserFriendData struct {
	Username string          `json:"username" jsonschema_description:"Username of the account being analyzed"`
	Friends  []FriendSummary `json:"friends"  jsonschema_description:"List of friends and their violation data"`
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
	chat        client.ChatCompletions
	minify      *minify.M
	analysisSem *semaphore.Weighted
	logger      *zap.Logger
	textLogger  *zap.Logger
	textDir     string
	model       string
	batchSize   int
}

// FriendAnalysisSchema is the JSON schema for the friend analysis response.
var FriendAnalysisSchema = utils.GenerateSchema[BatchFriendAnalysis]()

// NewFriendAnalyzer creates a FriendAnalyzer.
func NewFriendAnalyzer(app *setup.App, logger *zap.Logger) *FriendAnalyzer {
	// Create a minifier for JSON optimization
	m := minify.New()
	m.AddFunc(ApplicationJSON, json.Minify)

	// Get text logger
	textLogger, textDir, err := app.LogManager.GetTextLogger("friend_analyzer")
	if err != nil {
		logger.Error("Failed to create text logger", zap.Error(err))
		textLogger = logger
	}

	return &FriendAnalyzer{
		chat:        app.AIClient.Chat(),
		minify:      m,
		analysisSem: semaphore.NewWeighted(int64(app.Config.Worker.BatchSizes.FriendAnalysis)),
		logger:      logger.Named("ai_friend"),
		textLogger:  textLogger,
		textDir:     textDir,
		model:       app.Config.Common.OpenAI.FriendModel,
		batchSize:   app.Config.Worker.BatchSizes.FriendAnalysisBatch,
	}
}

// GenerateFriendReasons generates friend network analysis reasons for multiple users using the OpenAI model.
func (a *FriendAnalyzer) GenerateFriendReasons(
	userInfos []*types.ReviewUser, confirmedFriendsMap, flaggedFriendsMap map[uint64]map[uint64]*types.ReviewUser,
) map[uint64]string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	var (
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

// processFriendBatch handles the AI analysis for a batch of friend data.
func (a *FriendAnalyzer) processFriendBatch(ctx context.Context, batch []UserFriendData) (*BatchFriendAnalysis, error) {
	// Convert to JSON for the AI request
	batchDataJSON, err := sonic.Marshal(batch)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", utils.ErrJSONProcessing, err)
	}

	// Minify JSON to reduce token usage
	batchDataJSON, err = a.minify.Bytes(ApplicationJSON, batchDataJSON)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", utils.ErrJSONProcessing, err)
	}

	// Configure prompt for friend analysis
	prompt := fmt.Sprintf(FriendUserPrompt, string(batchDataJSON))

	// Prepare chat completion parameters
	params := openai.ChatCompletionNewParams{
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
	}

	// Make API request
	var result *BatchFriendAnalysis
	err = a.chat.NewWithRetry(ctx, params, func(resp *openai.ChatCompletion, err error) error {
		// Handle API error
		if err != nil {
			return fmt.Errorf("openai API error: %w", err)
		}

		// Check for empty response
		if len(resp.Choices) == 0 || len(resp.Choices[0].Message.Content) == 0 {
			return fmt.Errorf("%w: no response from model", utils.ErrModelResponse)
		}

		// Extract thought process
		message := resp.Choices[0].Message
		if thought := message.JSON.ExtraFields["reasoning"]; thought.IsPresent() {
			a.logger.Debug("AI friend analysis thought process",
				zap.String("model", resp.Model),
				zap.String("thought", thought.Raw()))
		}

		// Parse response from AI
		if err := sonic.Unmarshal([]byte(message.Content), &result); err != nil {
			return fmt.Errorf("JSON unmarshal error: %w", err)
		}

		return nil
	})

	return result, err
}

// processBatch handles analysis for a batch of users.
func (a *FriendAnalyzer) processBatch(
	ctx context.Context, userInfos []*types.ReviewUser,
	confirmedFriendsMap, flaggedFriendsMap map[uint64]map[uint64]*types.ReviewUser,
) ([]FriendAnalysis, error) {
	// Create summaries for all users in batch
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
				Type:        "Confirmed",
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
				Type:        "Flagged",
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

	// Acquire semaphore
	if err := a.analysisSem.Acquire(ctx, 1); err != nil {
		return nil, fmt.Errorf("failed to acquire semaphore: %w", err)
	}
	defer a.analysisSem.Release(1)

	// Handle content blocking
	minBatchSize := max(len(batchData)/4, 1)

	var result *BatchFriendAnalysis
	err := utils.WithRetrySplitBatch(
		ctx, batchData, len(batchData), minBatchSize, utils.GetAIRetryOptions(),
		func(batch []UserFriendData) error {
			var err error
			result, err = a.processFriendBatch(ctx, batch)
			return err
		},
		func(batch []UserFriendData) {
			usernames := make([]string, len(batch))
			for i, data := range batch {
				usernames[i] = data.Username
			}

			// Log detailed content to text logger
			a.textLogger.Warn("Content blocked in friend analysis batch",
				zap.Strings("usernames", usernames),
				zap.Int("batch_size", len(batch)),
				zap.Any("friend_data", batch))

			// Save blocked friend data to file
			filename := fmt.Sprintf("friends_%s.txt", time.Now().Format("20060102_150405"))
			filepath := filepath.Join(a.textDir, filename)

			var buf bytes.Buffer
			for _, data := range batch {
				buf.WriteString(fmt.Sprintf("Username: %s\nFriends:\n", data.Username))
				for _, friend := range data.Friends {
					buf.WriteString(fmt.Sprintf("  - Name: %s\n    Type: %s\n    Reasons: %v\n",
						friend.Name, friend.Type, friend.ReasonTypes))
				}
				buf.WriteString("\n")
			}

			if err := os.WriteFile(filepath, buf.Bytes(), 0o600); err != nil {
				a.textLogger.Error("Failed to save blocked friend data",
					zap.Error(err),
					zap.String("path", filepath))
				return
			}

			a.textLogger.Info("Saved blocked friend data",
				zap.String("path", filepath))
		},
	)
	if err != nil {
		return nil, err
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

package ai

import (
	"bytes"
	"context"
	"fmt"
	"maps"
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
	"github.com/tdewolff/minify/v2"
	"github.com/tdewolff/minify/v2/json"
	"go.uber.org/zap"
	"golang.org/x/sync/semaphore"
)

const (
	// MaxFriends is the maximum number of friends to include in analysis.
	MaxFriends = 10
)

// FriendSummary contains a summary of a friend's data.
type FriendSummary struct {
	Name    string             `json:"name"    jsonschema_description:"Username of the friend"`
	Type    string             `json:"type"    jsonschema_description:"Type of friend (Confirmed or Flagged)"`
	Reasons []types.ReasonInfo `json:"reasons" jsonschema_description:"List of reasons with types and messages why this friend was flagged"`
}

// UserFriendData represents the data for a user's friend network.
type UserFriendData struct {
	Username string          `json:"username" jsonschema_description:"Username of the account being analyzed"`
	Friends  []FriendSummary `json:"friends"  jsonschema_description:"List of friends and their violation data"`
}

// UserFriendRequest contains the user data and friend network for analysis.
type UserFriendRequest struct {
	UserInfo *types.ReviewUser `json:"-"`        // User info stored for internal reference, not sent to AI
	UserData UserFriendData    `json:"userData"` // Friend network data to be analyzed
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

// FriendReasonAnalyzer handles AI-based analysis of friend networks using OpenAI models.
type FriendReasonAnalyzer struct {
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

// NewFriendReasonAnalyzer creates a FriendReasonAnalyzer.
func NewFriendReasonAnalyzer(app *setup.App, logger *zap.Logger) *FriendReasonAnalyzer {
	// Create a minifier for JSON optimization
	m := minify.New()
	m.AddFunc(ApplicationJSON, json.Minify)

	// Get text logger
	textLogger, textDir, err := app.LogManager.GetTextLogger("friend_reason_analyzer")
	if err != nil {
		logger.Error("Failed to create text logger", zap.Error(err))
		textLogger = logger
	}

	return &FriendReasonAnalyzer{
		chat:        app.AIClient.Chat(),
		minify:      m,
		analysisSem: semaphore.NewWeighted(int64(app.Config.Worker.BatchSizes.FriendReasonAnalysis)),
		logger:      logger.Named("ai_friend_reason"),
		textLogger:  textLogger,
		textDir:     textDir,
		model:       app.Config.Common.OpenAI.FriendReasonModel,
		batchSize:   app.Config.Worker.BatchSizes.FriendReasonAnalysisBatch,
	}
}

// GenerateFriendReasons generates friend network analysis reasons for multiple users using the OpenAI model.
func (a *FriendReasonAnalyzer) GenerateFriendReasons(
	ctx context.Context, userInfos []*types.ReviewUser, confirmedFriendsMap, flaggedFriendsMap map[uint64]map[uint64]*types.ReviewUser,
) map[uint64]string {
	// Create friend requests map
	friendRequests := make(map[uint64]UserFriendRequest)
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
				Name:    friend.Name,
				Type:    "Confirmed",
				Reasons: friend.Reasons.ReasonInfos(),
			})
		}

		// Add flagged friends with remaining space
		for _, friend := range flaggedFriends {
			if len(friendSummaries) >= MaxFriends {
				break
			}
			friendSummaries = append(friendSummaries, FriendSummary{
				Name:    friend.Name,
				Type:    "Flagged",
				Reasons: friend.Reasons.ReasonInfos(),
			})
		}

		friendRequests[userInfo.ID] = UserFriendRequest{
			UserInfo: userInfo,
			UserData: UserFriendData{
				Username: userInfo.Name,
				Friends:  friendSummaries,
			},
		}
	}

	// Process friend requests
	results := make(map[uint64]string)
	a.ProcessFriendRequests(ctx, friendRequests, results)

	return results
}

// ProcessFriendRequests processes friend analysis requests with retry logic for invalid users.
func (a *FriendReasonAnalyzer) ProcessFriendRequests(
	ctx context.Context, friendRequests map[uint64]UserFriendRequest, results map[uint64]string,
) {
	if len(friendRequests) == 0 {
		return
	}

	// Convert map to slice for batch processing
	requestSlice := make([]UserFriendRequest, 0, len(friendRequests))
	for _, req := range friendRequests {
		requestSlice = append(requestSlice, req)
	}

	// Process batches with retry and splitting
	var (
		mu              sync.Mutex
		invalidMu       sync.Mutex
		invalidRequests = make(map[uint64]UserFriendRequest)
	)
	minBatchSize := max(len(requestSlice)/4, 1)

	err := utils.WithRetrySplitBatch(
		ctx, requestSlice, a.batchSize, minBatchSize, utils.GetAIRetryOptions(),
		func(batch []UserFriendRequest) error {
			// Process the batch
			batchResults, err := a.processFriendBatch(ctx, batch)
			if err != nil {
				return err
			}

			// Process and store valid results
			invalid := a.processResults(batchResults, batch, results, &mu)

			// Add invalid results to retry map
			if len(invalid) > 0 {
				invalidMu.Lock()
				maps.Copy(invalidRequests, invalid)
				invalidMu.Unlock()
			}

			return nil
		},
		func(batch []UserFriendRequest) {
			// Log blocked content
			usernames := make([]string, len(batch))
			for i, req := range batch {
				usernames[i] = req.UserData.Username
			}

			// Log details of the blocked content
			a.textLogger.Warn("Content blocked in friend analysis batch",
				zap.Strings("usernames", usernames),
				zap.Int("batch_size", len(batch)),
				zap.Any("requests", batch))

			// Save blocked friend data to file
			filename := fmt.Sprintf("friends_%s.txt", time.Now().Format("20060102_150405"))
			filepath := filepath.Join(a.textDir, filename)

			var buf bytes.Buffer
			for _, req := range batch {
				buf.WriteString(fmt.Sprintf("Username: %s\nFriends:\n", req.UserData.Username))
				for _, friend := range req.UserData.Friends {
					buf.WriteString(fmt.Sprintf("  - Name: %s\n    Type: %s\n    Reasons: %v\n",
						friend.Name, friend.Type, friend.Reasons))
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
		a.logger.Error("Error processing friend requests", zap.Error(err))
	}

	// Process invalid requests if any
	if len(invalidRequests) > 0 {
		a.logger.Info("Retrying analysis for invalid results",
			zap.Int("invalidUsers", len(invalidRequests)))

		a.ProcessFriendRequests(ctx, invalidRequests, results)
	}

	a.logger.Info("Finished processing friend requests",
		zap.Int("totalRequests", len(friendRequests)),
		zap.Int("retriedUsers", len(invalidRequests)))
}

// processFriendBatch handles the AI analysis for a batch of friend data.
func (a *FriendReasonAnalyzer) processFriendBatch(ctx context.Context, batch []UserFriendRequest) (*BatchFriendAnalysis, error) {
	// Extract UserFriendData for AI request
	batchData := make([]UserFriendData, len(batch))
	for i, req := range batch {
		batchData[i] = req.UserData
	}

	// Convert to JSON for the AI request
	batchDataJSON, err := sonic.Marshal(batchData)
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
		Temperature: openai.Float(0.0),
		TopP:        openai.Float(0.4),
	}

	// Make API request
	var result BatchFriendAnalysis
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
		if thought := message.JSON.ExtraFields["reasoning"]; thought.Valid() {
			a.logger.Debug("AI friend analysis thought process",
				zap.String("model", resp.Model),
				zap.String("thought", thought.Raw()))
		}

		// Parse response from AI
		if err := sonic.Unmarshal([]byte(message.Content), &result); err != nil {
			return fmt.Errorf("JSON unmarshal error: %w", err)
		}

		// Create a map of usernames to user IDs for efficient lookup
		userIDMap := make(map[string]uint64, len(batch))
		for _, req := range batch {
			userIDMap[req.UserData.Username] = req.UserInfo.ID
		}

		// Create a new slice to store the processed results
		processedResults := make([]FriendAnalysis, 0, len(result.Results))

		// Process each result and validate
		for _, response := range result.Results {
			// Skip responses with missing or empty usernames
			if response.Name == "" {
				a.logger.Warn("Received response with empty username")
				continue
			}

			// Skip responses with no analysis content
			if response.Analysis == "" {
				a.logger.Debug("Skipping response with empty analysis",
					zap.String("username", response.Name))
				continue
			}

			processedResults = append(processedResults, response)
		}

		result.Results = processedResults
		return nil
	})

	return &result, err
}

// processResults validates and stores the analysis results.
// Returns a map of user IDs that had invalid results and need retry.
func (a *FriendReasonAnalyzer) processResults(
	results *BatchFriendAnalysis, batch []UserFriendRequest, finalResults map[uint64]string, mu *sync.Mutex,
) map[uint64]UserFriendRequest {
	// Create map for retry requests
	invalidRequests := make(map[uint64]UserFriendRequest)

	// If no results returned, mark all users for retry
	if results == nil || len(results.Results) == 0 {
		a.logger.Warn("No results returned from friend analysis, retrying all users")
		for _, req := range batch {
			invalidRequests[req.UserInfo.ID] = req
		}
		return invalidRequests
	}

	// Create map of processed users for O(1) lookup
	processedUsers := make(map[string]struct{}, len(results.Results))
	for _, result := range results.Results {
		processedUsers[result.Name] = struct{}{}
	}

	// Create map of requests by username for O(1) lookup
	requestsByName := make(map[string]UserFriendRequest, len(batch))
	for _, req := range batch {
		requestsByName[req.UserData.Username] = req
	}

	// Handle missing users
	for _, req := range batch {
		if _, wasProcessed := processedUsers[req.UserData.Username]; !wasProcessed {
			a.logger.Warn("User missing from friend analysis results",
				zap.String("username", req.UserData.Username))
			invalidRequests[req.UserInfo.ID] = req
		}
	}

	// Process valid results
	for _, result := range results.Results {
		// Get the original request
		req, exists := requestsByName[result.Name]
		if !exists {
			a.logger.Error("Got result for user not in batch",
				zap.String("username", result.Name))
			continue
		}

		// Skip results with no analysis content
		if result.Analysis == "" {
			a.logger.Debug("Friend analysis returned empty results",
				zap.String("username", result.Name))
			invalidRequests[req.UserInfo.ID] = req
			continue
		}

		// Store valid result
		mu.Lock()
		finalResults[req.UserInfo.ID] = result.Analysis
		mu.Unlock()

		a.logger.Debug("Added friend analysis result",
			zap.String("username", result.Name))
	}

	return invalidRequests
}

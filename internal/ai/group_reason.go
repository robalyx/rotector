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

	"github.com/alpkeskin/gotoon"
	"github.com/bytedance/sonic"
	"github.com/openai/openai-go"
	"github.com/robalyx/rotector/internal/ai/client"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/setup"
	"github.com/robalyx/rotector/pkg/utils"
	"go.uber.org/zap"
	"golang.org/x/sync/semaphore"
)

const (
	// MaxGroups is the maximum number of groups to include in analysis.
	MaxGroups = 6
	// GroupReasonMaxRetries is the maximum number of retry attempts for group reason analysis.
	GroupReasonMaxRetries = 3
)

// GroupSummary contains a summary of a group's data.
type GroupSummary struct {
	Name    string             `json:"name"    jsonschema:"required,minLength=1,description=Name of the group"`
	Type    string             `json:"type"    jsonschema:"required,enum=Confirmed,enum=Flagged,enum=Mixed,description=Type of group (Confirmed or Flagged or Mixed)"`
	Reasons []types.ReasonInfo `json:"reasons" jsonschema:"required,maxItems=20,description=List of reasons with types and messages why this group was flagged"`
}

// UserGroupData represents the data for a user's group memberships.
type UserGroupData struct {
	Username string         `json:"username" jsonschema:"required,minLength=1,description=Username of the account being analyzed"`
	Groups   []GroupSummary `json:"groups"   jsonschema:"required,maxItems=100,description=List of groups and their violation data"`
}

// UserGroupRequest contains the user data and group memberships for analysis.
type UserGroupRequest struct {
	UserInfo *types.ReviewUser `json:"-"`        // User info stored for internal reference, not sent to AI
	UserData UserGroupData     `json:"userData"` // Group membership data to be analyzed
}

// GroupAnalysis contains the result of analyzing a user's group memberships.
type GroupAnalysis struct {
	Name     string `json:"name"     jsonschema:"required,minLength=1,description=Username of the account being analyzed"`
	Analysis string `json:"analysis" jsonschema:"required,minLength=1,description=Analysis of group membership patterns for this user"`
}

// BatchGroupAnalysis contains results for multiple users' group memberships.
type BatchGroupAnalysis struct {
	Results []GroupAnalysis `json:"results" jsonschema:"required,maxItems=50,description=Array of group membership analyses for each user"`
}

// GroupReasonAnalyzer handles AI-based analysis of group memberships using OpenAI models.
type GroupReasonAnalyzer struct {
	chat          client.ChatCompletions
	analysisSem   *semaphore.Weighted
	logger        *zap.Logger
	textLogger    *zap.Logger
	textDir       string
	model         string
	fallbackModel string
	batchSize     int
}

// GroupAnalysisSchema is the JSON schema for the group analysis response.
var GroupAnalysisSchema = utils.GenerateSchema[BatchGroupAnalysis]()

// NewGroupReasonAnalyzer creates a GroupReasonAnalyzer.
func NewGroupReasonAnalyzer(app *setup.App, logger *zap.Logger) *GroupReasonAnalyzer {
	// Get text logger
	textLogger, textDir, err := app.LogManager.GetTextLogger("group_reason_analyzer")
	if err != nil {
		logger.Error("Failed to create text logger", zap.Error(err))
		textLogger = logger
	}

	return &GroupReasonAnalyzer{
		chat:          app.AIClient.Chat(),
		analysisSem:   semaphore.NewWeighted(int64(app.Config.Worker.BatchSizes.GroupReasonAnalysis)),
		logger:        logger.Named("ai_group_reason"),
		textLogger:    textLogger,
		textDir:       textDir,
		model:         app.Config.Common.OpenAI.GroupReasonModel,
		fallbackModel: app.Config.Common.OpenAI.GroupReasonFallbackModel,
		batchSize:     app.Config.Worker.BatchSizes.GroupReasonAnalysisBatch,
	}
}

// GenerateGroupReasons generates group membership analysis reasons for multiple users using the OpenAI model.
func (a *GroupReasonAnalyzer) GenerateGroupReasons(
	ctx context.Context, userInfos []*types.ReviewUser,
	confirmedGroupsMap, flaggedGroupsMap, mixedGroupsMap map[int64]map[int64]*types.ReviewGroup,
) map[int64]string {
	// Create group requests map
	groupRequests := make(map[int64]UserGroupRequest)

	for _, userInfo := range userInfos {
		// Get all group types for this user
		confirmedGroups := confirmedGroupsMap[userInfo.ID]
		flaggedGroups := flaggedGroupsMap[userInfo.ID]
		mixedGroups := mixedGroupsMap[userInfo.ID]

		// Collect group summaries
		groupSummaries := make([]GroupSummary, 0, MaxGroups)

		// Process confirmed groups first
		for _, group := range confirmedGroups {
			if len(groupSummaries) >= MaxGroups {
				break
			}

			groupSummaries = append(groupSummaries, GroupSummary{
				Name:    group.Name,
				Type:    "Confirmed",
				Reasons: group.Reasons.ReasonInfos(),
			})
		}

		// Process mixed groups
		for _, group := range mixedGroups {
			if len(groupSummaries) >= MaxGroups {
				break
			}

			groupSummaries = append(groupSummaries, GroupSummary{
				Name:    group.Name,
				Type:    "Mixed",
				Reasons: group.Reasons.ReasonInfos(),
			})
		}

		// Process flagged groups
		for _, group := range flaggedGroups {
			if len(groupSummaries) >= MaxGroups {
				break
			}

			groupSummaries = append(groupSummaries, GroupSummary{
				Name:    group.Name,
				Type:    "Flagged",
				Reasons: group.Reasons.ReasonInfos(),
			})
		}

		groupRequests[userInfo.ID] = UserGroupRequest{
			UserInfo: userInfo,
			UserData: UserGroupData{
				Username: userInfo.Name,
				Groups:   groupSummaries,
			},
		}
	}

	// Process group requests
	results := make(map[int64]string)
	a.ProcessGroupRequests(ctx, groupRequests, results, 0)

	return results
}

// ProcessGroupRequests processes group analysis requests with retry logic for invalid users.
func (a *GroupReasonAnalyzer) ProcessGroupRequests(
	ctx context.Context, groupRequests map[int64]UserGroupRequest, results map[int64]string, retryCount int,
) {
	if len(groupRequests) == 0 {
		return
	}

	// Prevent infinite retries
	if retryCount > GroupReasonMaxRetries {
		a.logger.Warn("Maximum retries reached for group analysis, skipping remaining users",
			zap.Int("retryCount", retryCount),
			zap.Int("maxRetries", GroupReasonMaxRetries),
			zap.Int("remainingUsers", len(groupRequests)))

		return
	}

	// Convert map to slice for batch processing
	requestSlice := make([]UserGroupRequest, 0, len(groupRequests))
	for _, req := range groupRequests {
		requestSlice = append(requestSlice, req)
	}

	// Process batches with retry and splitting
	var (
		mu              sync.Mutex
		invalidMu       sync.Mutex
		invalidRequests = make(map[int64]UserGroupRequest)
	)

	minBatchSize := max(len(requestSlice)/4, 1)

	err := utils.WithRetrySplitBatch(
		ctx, requestSlice, a.batchSize, minBatchSize, utils.GetAIRetryOptions(),
		func(batch []UserGroupRequest) error {
			// Process the batch
			batchResults, err := a.processGroupBatch(ctx, batch)
			if err != nil {
				invalidMu.Lock()

				for _, req := range batch {
					invalidRequests[req.UserInfo.ID] = req
				}

				invalidMu.Unlock()

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
		func(batch []UserGroupRequest) {
			// Log blocked content
			usernames := make([]string, len(batch))
			for i, req := range batch {
				usernames[i] = req.UserData.Username
			}

			// Log details of the blocked content
			a.textLogger.Warn("Content blocked in group analysis batch",
				zap.Strings("usernames", usernames),
				zap.Int("batch_size", len(batch)),
				zap.Any("requests", batch))

			// Save blocked group data to file
			filename := fmt.Sprintf("groups_%s.txt", time.Now().Format("20060102_150405"))
			filePath := filepath.Join(a.textDir, filename)

			var buf bytes.Buffer
			for _, req := range batch {
				buf.WriteString(fmt.Sprintf("Username: %s\nGroups:\n", req.UserData.Username))

				for _, group := range req.UserData.Groups {
					buf.WriteString(fmt.Sprintf("  - Name: %s\n    Type: %s\n    Reasons: %v\n",
						group.Name, group.Type, group.Reasons))
				}

				buf.WriteString("\n")
			}

			if err := os.WriteFile(filePath, buf.Bytes(), 0o600); err != nil {
				a.textLogger.Error("Failed to save blocked group data",
					zap.Error(err),
					zap.String("path", filePath))

				return
			}

			a.textLogger.Info("Saved blocked group data",
				zap.String("path", filePath))
		},
	)
	if err != nil {
		a.logger.Error("Error processing group requests", zap.Error(err))
	}

	// Process invalid requests if any
	if len(invalidRequests) > 0 {
		a.logger.Info("Retrying analysis for invalid results",
			zap.Int("invalidUsers", len(invalidRequests)),
			zap.Int("retryCount", retryCount))

		a.ProcessGroupRequests(ctx, invalidRequests, results, retryCount+1)
	}

	a.logger.Info("Finished processing group requests",
		zap.Int("totalRequests", len(groupRequests)),
		zap.Int("retriedUsers", len(invalidRequests)),
		zap.Int("retryCount", retryCount))
}

// processGroupBatch handles the AI analysis for a batch of group data.
func (a *GroupReasonAnalyzer) processGroupBatch(ctx context.Context, batch []UserGroupRequest) (*BatchGroupAnalysis, error) {
	// Extract UserGroupData for AI request
	batchData := make([]UserGroupData, len(batch))
	for i, req := range batch {
		batchData[i] = req.UserData
	}

	// Convert to TOON format
	toonData, err := gotoon.Encode(batchData)
	if err != nil {
		return nil, fmt.Errorf("TOON marshal error: %w", err)
	}

	// Configure prompt for group analysis
	prompt := fmt.Sprintf(GroupUserPrompt, toonData)

	// Prepare chat completion parameters
	params := openai.ChatCompletionNewParams{
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(GroupSystemPrompt),
			openai.UserMessage(prompt),
		},
		ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONSchema: &openai.ResponseFormatJSONSchemaParam{
				JSONSchema: openai.ResponseFormatJSONSchemaJSONSchemaParam{
					Name:        "groupAnalysis",
					Description: openai.String("Analysis of group membership patterns"),
					Schema:      GroupAnalysisSchema,
					Strict:      openai.Bool(true),
				},
			},
		},
		Model:       a.model,
		Temperature: openai.Float(0.0),
		TopP:        openai.Float(0.4),
	}

	// Configure extra fields for model
	params.SetExtraFields(client.NewExtraFieldsSettings().ForModel(a.model).Build())

	// Make API request
	var result BatchGroupAnalysis

	err = a.chat.NewWithRetryAndFallback(ctx, params, a.fallbackModel, func(resp *openai.ChatCompletion, err error) error {
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
			a.logger.Debug("AI group analysis thought process",
				zap.String("model", resp.Model),
				zap.String("thought", thought.Raw()))
		}

		// Parse response from AI
		if err := sonic.Unmarshal([]byte(message.Content), &result); err != nil {
			return fmt.Errorf("JSON unmarshal error: %w", err)
		}

		// Create a map of usernames to user IDs for efficient lookup
		userIDMap := make(map[string]int64, len(batch))
		for _, req := range batch {
			userIDMap[req.UserData.Username] = req.UserInfo.ID
		}

		// Create a new slice to store the processed results
		processedResults := make([]GroupAnalysis, 0, len(result.Results))

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
func (a *GroupReasonAnalyzer) processResults(
	results *BatchGroupAnalysis, batch []UserGroupRequest, finalResults map[int64]string, mu *sync.Mutex,
) map[int64]UserGroupRequest {
	// Create map for retry requests
	invalidRequests := make(map[int64]UserGroupRequest)

	// If no results returned, mark all users for retry
	if results == nil || len(results.Results) == 0 {
		a.logger.Warn("No results returned from group analysis, retrying all users")

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
	requestsByName := make(map[string]UserGroupRequest, len(batch))
	for _, req := range batch {
		requestsByName[req.UserData.Username] = req
	}

	// Handle missing users
	for _, req := range batch {
		if _, wasProcessed := processedUsers[req.UserData.Username]; !wasProcessed {
			a.logger.Warn("User missing from group analysis results",
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
			a.logger.Debug("Group analysis returned empty results",
				zap.String("username", result.Name))

			invalidRequests[req.UserInfo.ID] = req

			continue
		}

		// Store valid result
		mu.Lock()

		finalResults[req.UserInfo.ID] = result.Analysis

		mu.Unlock()

		a.logger.Debug("Added group analysis result",
			zap.String("username", result.Name))
	}

	return invalidRequests
}

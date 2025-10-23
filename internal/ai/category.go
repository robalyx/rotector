package ai

import (
	"context"
	"fmt"
	"maps"
	"strings"
	"sync"

	"github.com/bytedance/sonic"
	"github.com/openai/openai-go"
	"github.com/robalyx/rotector/internal/ai/client"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/database/types/enum"
	"github.com/robalyx/rotector/internal/setup"
	"github.com/robalyx/rotector/pkg/utils"
	"github.com/tdewolff/minify/v2"
	"github.com/tdewolff/minify/v2/json"
	"go.uber.org/zap"
	"golang.org/x/sync/semaphore"
)

const (
	// CategoryMaxRetries is the maximum number of retry attempts for category analysis.
	CategoryMaxRetries = 2
)

// CategoryRequest contains the user data and reasons for classification.
type CategoryRequest struct {
	Username string              `json:"username"` // Username for identification
	Reasons  map[string][]string `json:"reasons"`  // Map of reason types to their messages
	UserID   int64               `json:"-"`        // User ID stored for internal reference, not sent to AI
}

// CategoryResponse contains the category classification result.
type CategoryResponse struct {
	Username string `json:"username" jsonschema_description:"Username of the classified user"`
	Category string `json:"category" jsonschema_description:"Violation category type"`
}

// CategoryAnalysisResult contains the analysis results for a batch of users.
type CategoryAnalysisResult struct {
	Results []CategoryResponse `json:"results" jsonschema_description:"List of user category classifications"`
}

// CategoryAnalyzer classifies flagged users into violation categories.
type CategoryAnalyzer struct {
	chat          client.ChatCompletions
	minify        *minify.M
	analysisSem   *semaphore.Weighted
	logger        *zap.Logger
	model         string
	fallbackModel string
	batchSize     int
}

// CategoryAnalysisSchema is the JSON schema for the category analysis response.
var CategoryAnalysisSchema = utils.GenerateSchema[CategoryAnalysisResult]()

// NewCategoryAnalyzer creates a new CategoryAnalyzer.
func NewCategoryAnalyzer(app *setup.App, logger *zap.Logger) *CategoryAnalyzer {
	// Create a minifier for JSON optimization
	m := minify.New()
	m.AddFunc(ApplicationJSON, json.Minify)

	return &CategoryAnalyzer{
		chat:          app.AIClient.Chat(),
		minify:        m,
		analysisSem:   semaphore.NewWeighted(int64(app.Config.Worker.BatchSizes.CategoryAnalysis)),
		logger:        logger.Named("ai_category"),
		model:         app.Config.Common.OpenAI.CategoryModel,
		fallbackModel: app.Config.Common.OpenAI.CategoryFallbackModel,
		batchSize:     app.Config.Worker.BatchSizes.CategoryAnalysisBatch,
	}
}

// ClassifyUsers classifies users into violation categories based on their reasons.
func (a *CategoryAnalyzer) ClassifyUsers(
	ctx context.Context, users map[int64]*types.ReviewUser, retryCount int,
) map[int64]enum.UserCategoryType {
	if len(users) == 0 {
		return make(map[int64]enum.UserCategoryType)
	}

	// Prevent infinite retries
	if retryCount > CategoryMaxRetries {
		a.logger.Warn("Maximum retries reached for category analysis, skipping remaining users",
			zap.Int("retryCount", retryCount),
			zap.Int("maxRetries", CategoryMaxRetries),
			zap.Int("remainingUsers", len(users)))

		return make(map[int64]enum.UserCategoryType)
	}

	// Build category requests only for users with reasons
	categoryRequests := make(map[int64]CategoryRequest)

	for userID, user := range users {
		if len(user.Reasons) == 0 {
			continue
		}

		// Convert reasons to a map of reason types to messages
		reasonsMap := make(map[string][]string)

		for reasonType, reason := range user.Reasons {
			reasonTypeStr := reasonType.String()
			if reasonsMap[reasonTypeStr] == nil {
				reasonsMap[reasonTypeStr] = make([]string, 0)
			}

			reasonsMap[reasonTypeStr] = append(reasonsMap[reasonTypeStr], reason.Message)
		}

		categoryRequests[userID] = CategoryRequest{
			Username: user.Name,
			Reasons:  reasonsMap,
			UserID:   userID,
		}
	}

	if len(categoryRequests) == 0 {
		a.logger.Debug("No users with reasons to classify")
		return make(map[int64]enum.UserCategoryType)
	}

	// Convert map to slice for batch processing
	requestSlice := make([]CategoryRequest, 0, len(categoryRequests))
	for _, req := range categoryRequests {
		requestSlice = append(requestSlice, req)
	}

	// Process batches with retry and splitting
	var (
		mu              sync.Mutex
		invalidMu       sync.Mutex
		categoryResults = make(map[int64]enum.UserCategoryType)
		invalidRequests = make(map[int64]CategoryRequest)
	)

	minBatchSize := max(len(requestSlice)/4, 1)

	err := utils.WithRetrySplitBatch(
		ctx, requestSlice, a.batchSize, minBatchSize, utils.GetAIRetryOptions(),
		func(batch []CategoryRequest) error {
			// Process the batch
			results, err := a.processCategoryBatch(ctx, batch)
			if err != nil {
				return err
			}

			// Process and store valid results
			invalid := a.processResults(results, batch, categoryResults, &mu)

			// Add invalid results to retry map
			if len(invalid) > 0 {
				invalidMu.Lock()
				maps.Copy(invalidRequests, invalid)
				invalidMu.Unlock()
			}

			return nil
		},
		func(batch []CategoryRequest) {
			// Log blocked content
			usernames := make([]string, len(batch))
			for i, req := range batch {
				usernames[i] = req.Username
			}

			a.logger.Warn("Content blocked in category analysis batch",
				zap.Strings("usernames", usernames),
				zap.Int("batch_size", len(batch)))
		},
	)
	if err != nil {
		a.logger.Error("Error processing category analysis", zap.Error(err))
	}

	// Process invalid requests if any
	if len(invalidRequests) > 0 {
		a.logger.Info("Retrying category analysis for invalid results",
			zap.Int("invalidUsers", len(invalidRequests)),
			zap.Int("retryCount", retryCount))

		// Create users map for retry
		retryUsers := make(map[int64]*types.ReviewUser)

		for userID := range invalidRequests {
			if user, exists := users[userID]; exists {
				retryUsers[userID] = user
			}
		}

		retryResults := a.ClassifyUsers(ctx, retryUsers, retryCount+1)

		// Merge retry results
		mu.Lock()
		maps.Copy(categoryResults, retryResults)
		mu.Unlock()
	}

	a.logger.Info("Finished classifying users into categories",
		zap.Int("totalUsers", len(users)),
		zap.Int("classified", len(categoryResults)),
		zap.Int("retryAttempt", retryCount))

	return categoryResults
}

// processCategoryBatch handles the AI analysis for a batch of users.
func (a *CategoryAnalyzer) processCategoryBatch(ctx context.Context, batch []CategoryRequest) (*CategoryAnalysisResult, error) {
	// Convert to JSON
	reqJSON, err := sonic.Marshal(batch)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", utils.ErrJSONProcessing, err)
	}

	// Minify JSON to reduce token usage
	reqJSON, err = a.minify.Bytes(ApplicationJSON, reqJSON)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", utils.ErrJSONProcessing, err)
	}

	// Prepare request prompt
	requestPrompt := CategoryRequestPrompt + string(reqJSON)

	// Prepare chat completion parameters
	params := openai.ChatCompletionNewParams{
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(CategorySystemPrompt),
			openai.UserMessage(requestPrompt),
		},
		ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONSchema: &openai.ResponseFormatJSONSchemaParam{
				JSONSchema: openai.ResponseFormatJSONSchemaJSONSchemaParam{
					Name:        "categoryAnalysis",
					Description: openai.String("User violation category classification"),
					Schema:      CategoryAnalysisSchema,
					Strict:      openai.Bool(true),
				},
			},
		},
		Model:       a.model,
		Temperature: openai.Float(0.0),
		TopP:        openai.Float(0.2),
	}

	// Make API request
	var result CategoryAnalysisResult

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
			a.logger.Debug("AI category analysis thought process",
				zap.String("model", resp.Model),
				zap.String("thought", thought.Raw()))
		}

		// Parse response from AI
		if err := sonic.Unmarshal([]byte(message.Content), &result); err != nil {
			return fmt.Errorf("JSON unmarshal error: %w", err)
		}

		processedResults := make([]CategoryResponse, 0, len(result.Results))

		// Process each result
		for _, response := range result.Results {
			// Skip responses with missing or empty usernames
			if response.Username == "" {
				a.logger.Warn("Received response with empty username")
				continue
			}

			// Skip responses with no category
			if response.Category == "" {
				a.logger.Debug("Skipping response with empty category",
					zap.String("username", response.Username))

				continue
			}

			processedResults = append(processedResults, response)
		}

		result.Results = processedResults

		return nil
	})
	if err != nil {
		return nil, err
	}

	return &result, nil
}

// processResults validates and stores the analysis results.
// Returns a map of usernames that had invalid results and need retry.
func (a *CategoryAnalyzer) processResults(
	results *CategoryAnalysisResult, batch []CategoryRequest,
	categoryResults map[int64]enum.UserCategoryType, mu *sync.Mutex,
) map[int64]CategoryRequest {
	// Create map for retry requests
	invalidRequests := make(map[int64]CategoryRequest)

	// If no results returned, mark all users for retry
	if results == nil || len(results.Results) == 0 {
		a.logger.Warn("No results returned from category analysis, retrying all users")

		for _, req := range batch {
			invalidRequests[req.UserID] = req
		}

		return invalidRequests
	}

	// Create map of processed users
	processedUsers := make(map[string]struct{}, len(results.Results))
	for _, result := range results.Results {
		processedUsers[result.Username] = struct{}{}
	}

	// Create map of requests by username
	requestsByName := make(map[string]CategoryRequest, len(batch))
	for _, req := range batch {
		requestsByName[req.Username] = req
	}

	// Handle missing users
	for _, req := range batch {
		if _, wasProcessed := processedUsers[req.Username]; !wasProcessed {
			a.logger.Warn("User missing from category analysis results",
				zap.String("username", req.Username))
			invalidRequests[req.UserID] = req
		}
	}

	// Process valid results
	for _, result := range results.Results {
		// Get the original request
		req, exists := requestsByName[result.Username]
		if !exists {
			a.logger.Error("Got result for user not in batch",
				zap.String("username", result.Username))

			continue
		}

		// Parse category string to enum
		var category enum.UserCategoryType

		switch strings.ToLower(result.Category) {
		case "predatory":
			category = enum.UserCategoryTypePredatory
		case "csam":
			category = enum.UserCategoryTypeCSAM
		case "sexual":
			category = enum.UserCategoryTypeSexual
		case "kink":
			category = enum.UserCategoryTypeKink
		case "raceplay":
			category = enum.UserCategoryTypeRaceplay
		case "condo":
			category = enum.UserCategoryTypeCondo
		case "other":
			category = enum.UserCategoryTypeOther
		default:
			a.logger.Warn("Unknown category returned",
				zap.String("username", result.Username),
				zap.String("category", result.Category))

			invalidRequests[req.UserID] = req

			continue
		}

		// Store the classification
		mu.Lock()

		categoryResults[req.UserID] = category

		mu.Unlock()

		a.logger.Debug("Classified user category",
			zap.Int64("userID", req.UserID),
			zap.String("username", result.Username),
			zap.String("category", result.Category))
	}

	return invalidRequests
}

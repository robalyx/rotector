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
	"github.com/robalyx/rotector/internal/database/types/enum"
	"github.com/robalyx/rotector/internal/setup"
	"github.com/robalyx/rotector/pkg/utils"
	"github.com/tdewolff/minify/v2"
	"github.com/tdewolff/minify/v2/json"
	"go.uber.org/zap"
	"golang.org/x/sync/semaphore"
)

const (
	// UserReasonMaxRetries is the maximum number of retry attempts for user reason analysis.
	UserReasonMaxRetries = 2
)

// UserReasonRequest contains the flagged user data and the hinting needed.
type UserReasonRequest struct {
	User            *UserSummary `json:"user"`                      // User summary data
	Confidence      float64      `json:"confidence"`                // Original confidence from first-pass analysis
	Hint            string       `json:"hint"`                      // Clean hint about the violation type
	FlaggedFields   []string     `json:"flaggedFields,omitempty"`   // Profile fields containing violations
	LanguagePattern []string     `json:"languagePattern,omitempty"` // Linguistic patterns detected
	LanguageUsed    []string     `json:"languageUsed,omitempty"`    // Languages or encodings detected in content
	UserID          int64        `json:"-"`                         // User ID stored for internal reference, not sent to AI
}

// UserReasonResponse contains the detailed reason analysis with evidence.
type UserReasonResponse struct {
	Name           string   `json:"name"           jsonschema:"required,minLength=1,description=Username of the flagged user"`
	Reason         string   `json:"reason"         jsonschema:"required,minLength=1,description=Detailed explanation of the violation"`
	FlaggedContent []string `json:"flaggedContent" jsonschema:"required,maxItems=10,description=Specific content that violates policies"`
}

// ReasonAnalysisResult contains the analysis results for a batch of users.
type ReasonAnalysisResult struct {
	Results []UserReasonResponse `json:"results" jsonschema:"required,maxItems=50,description=List of detailed user analysis results"`
}

// UserReasonAnalyzer generates detailed reasons and evidence for flagged users.
type UserReasonAnalyzer struct {
	chat          client.ChatCompletions
	minify        *minify.M
	analysisSem   *semaphore.Weighted
	logger        *zap.Logger
	textLogger    *zap.Logger
	textDir       string
	model         string
	fallbackModel string
	batchSize     int
}

// UserReasonAnalysisSchema is the JSON schema for the reason analysis response.
var UserReasonAnalysisSchema = utils.GenerateSchema[ReasonAnalysisResult]()

// NewUserReasonAnalyzer creates a new UserReasonAnalyzer.
func NewUserReasonAnalyzer(app *setup.App, logger *zap.Logger) *UserReasonAnalyzer {
	// Create a minifier for JSON optimization
	m := minify.New()
	m.AddFunc(ApplicationJSON, json.Minify)

	// Get text logger
	textLogger, textDir, err := app.LogManager.GetTextLogger("user_reason_analyzer")
	if err != nil {
		logger.Error("Failed to create text logger", zap.Error(err))
		textLogger = logger
	}

	return &UserReasonAnalyzer{
		chat:          app.AIClient.Chat(),
		minify:        m,
		analysisSem:   semaphore.NewWeighted(int64(app.Config.Worker.BatchSizes.UserReasonAnalysis)),
		logger:        logger.Named("ai_user_reason"),
		textLogger:    textLogger,
		textDir:       textDir,
		model:         app.Config.Common.OpenAI.UserReasonModel,
		fallbackModel: app.Config.Common.OpenAI.UserReasonFallbackModel,
		batchSize:     app.Config.Worker.BatchSizes.UserReasonAnalysisBatch,
	}
}

// ProcessFlaggedUsers generates detailed reasons and evidence for users flagged.
func (a *UserReasonAnalyzer) ProcessFlaggedUsers(
	ctx context.Context, userReasonRequests map[int64]UserReasonRequest, translatedInfos map[string]*types.ReviewUser,
	originalInfos map[string]*types.ReviewUser, reasonsMap map[int64]types.Reasons[enum.UserReasonType],
	retryCount int,
) {
	if len(userReasonRequests) == 0 {
		return
	}

	// Prevent infinite retries
	if retryCount > UserReasonMaxRetries {
		a.logger.Warn("Maximum retries reached for user reason analysis, skipping remaining users",
			zap.Int("retryCount", retryCount),
			zap.Int("maxRetries", UserReasonMaxRetries),
			zap.Int("remainingUsers", len(userReasonRequests)))

		return
	}

	// Convert map to slice for batch processing
	requestSlice := make([]UserReasonRequest, 0, len(userReasonRequests))
	for _, req := range userReasonRequests {
		requestSlice = append(requestSlice, req)
	}

	// Process batches with retry and splitting
	var (
		mu              sync.Mutex
		invalidMu       sync.Mutex
		invalidRequests = make(map[int64]UserReasonRequest)
	)

	minBatchSize := max(len(requestSlice)/4, 1)

	err := utils.WithRetrySplitBatch(
		ctx, requestSlice, a.batchSize, minBatchSize, utils.GetAIRetryOptions(),
		func(batch []UserReasonRequest) error {
			// Process the batch
			results, err := a.processReasonBatch(ctx, batch)
			if err != nil {
				invalidMu.Lock()

				for _, req := range batch {
					invalidRequests[req.UserID] = req
				}

				invalidMu.Unlock()

				return err
			}

			// Process and store valid results
			invalid := a.processResults(results, batch, translatedInfos, originalInfos, reasonsMap, &mu)

			// Add invalid results to retry map
			if len(invalid) > 0 {
				invalidMu.Lock()
				maps.Copy(invalidRequests, invalid)
				invalidMu.Unlock()
			}

			return nil
		},
		func(batch []UserReasonRequest) {
			// Log blocked content
			usernames := make([]string, len(batch))
			for i, req := range batch {
				usernames[i] = req.User.Name
			}

			// Log details of the blocked content
			a.textLogger.Warn("Content blocked in user reason analysis batch",
				zap.Strings("usernames", usernames),
				zap.Int("batch_size", len(batch)),
				zap.Any("requests", batch))

			// Save blocked user data to file
			filename := fmt.Sprintf("reason_requests_%s.txt", time.Now().Format("20060102_150405"))
			filePath := filepath.Join(a.textDir, filename)

			var buf bytes.Buffer
			for _, req := range batch {
				buf.WriteString(fmt.Sprintf("Username: %s\n", req.User.Name))

				if req.User.DisplayName != "" && req.User.DisplayName != req.User.Name {
					buf.WriteString(fmt.Sprintf("Display Name: %s\n", req.User.DisplayName))
				}

				buf.WriteString(fmt.Sprintf("Description: %s\n", req.User.Description))
				buf.WriteString(fmt.Sprintf("Hint: %s\n", req.Hint))
				buf.WriteString(fmt.Sprintf("Confidence: %.2f\n\n", req.Confidence))
			}

			if err := os.WriteFile(filePath, buf.Bytes(), 0o600); err != nil {
				a.textLogger.Error("Failed to save blocked user reason data",
					zap.Error(err),
					zap.String("path", filePath))

				return
			}

			a.textLogger.Info("Saved blocked user reason data",
				zap.String("path", filePath))
		},
	)
	if err != nil {
		a.logger.Error("Error processing user reasons", zap.Error(err))
	}

	// Process invalid requests if any
	if len(invalidRequests) > 0 {
		a.logger.Info("Retrying analysis for invalid results",
			zap.Int("invalidUsers", len(invalidRequests)),
			zap.Int("retryCount", retryCount))

		a.ProcessFlaggedUsers(ctx, invalidRequests, translatedInfos, originalInfos, reasonsMap, retryCount+1)
	}

	a.logger.Info("Finished processing flagged users for detailed reasons",
		zap.Int("flaggedUsers", len(userReasonRequests)),
		zap.Int("retryAttempt", retryCount))
}

// processReasonBatch handles the AI analysis for a batch of flagged users.
func (a *UserReasonAnalyzer) processReasonBatch(ctx context.Context, batch []UserReasonRequest) (*ReasonAnalysisResult, error) {
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

	// Prepare request prompt with user info
	requestPrompt := UserReasonRequestPrompt + string(reqJSON)

	// Prepare chat completion parameters
	params := openai.ChatCompletionNewParams{
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(UserReasonSystemPrompt),
			openai.UserMessage(requestPrompt),
		},
		ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONSchema: &openai.ResponseFormatJSONSchemaParam{
				JSONSchema: openai.ResponseFormatJSONSchemaJSONSchemaParam{
					Name:        "userReasonAnalysis",
					Description: openai.String("Detailed analysis with reasons and evidence"),
					Schema:      UserReasonAnalysisSchema,
					Strict:      openai.Bool(true),
				},
			},
		},
		Model:       a.model,
		Temperature: openai.Float(0.0),
		TopP:        openai.Float(0.2),
	}

	// Make API request
	var result ReasonAnalysisResult

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
			a.logger.Debug("AI user reason analysis thought process",
				zap.String("model", resp.Model),
				zap.String("thought", thought.Raw()))
		}

		// Parse response from AI
		if err := sonic.Unmarshal([]byte(message.Content), &result); err != nil {
			return fmt.Errorf("JSON unmarshal error: %w", err)
		}

		processedResults := make([]UserReasonResponse, 0, len(result.Results))

		// Process each result
		for _, response := range result.Results {
			// Skip responses with missing or empty usernames
			if response.Name == "" {
				a.logger.Warn("Received response with empty username")
				continue
			}

			// Skip responses with no content
			if response.Reason == "" || len(response.FlaggedContent) == 0 {
				a.logger.Debug("Skipping response with empty reason or flagged content",
					zap.String("username", response.Name))

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
func (a *UserReasonAnalyzer) processResults(
	results *ReasonAnalysisResult, batch []UserReasonRequest, translatedInfos map[string]*types.ReviewUser,
	originalInfos map[string]*types.ReviewUser, reasonsMap map[int64]types.Reasons[enum.UserReasonType],
	mu *sync.Mutex,
) map[int64]UserReasonRequest {
	// Create map for retry requests
	invalidRequests := make(map[int64]UserReasonRequest)

	// If no results returned, mark all users for retry
	if results == nil || len(results.Results) == 0 {
		a.logger.Warn("No results returned from reason analysis, retrying all users")

		for _, req := range batch {
			invalidRequests[req.UserID] = req
		}

		return invalidRequests
	}

	// Create map of processed users for O(1) lookup
	processedUsers := make(map[string]struct{}, len(results.Results))
	for _, result := range results.Results {
		processedUsers[result.Name] = struct{}{}
	}

	// Create map of requests by username for O(1) lookup
	requestsByName := make(map[string]UserReasonRequest, len(batch))
	for _, req := range batch {
		requestsByName[req.User.Name] = req
	}

	// Handle missing users
	for _, req := range batch {
		if _, wasProcessed := processedUsers[req.User.Name]; !wasProcessed {
			a.logger.Warn("User missing from analysis results",
				zap.String("username", req.User.Name))
			invalidRequests[req.UserID] = req
		}
	}

	normalizer := utils.NewTextNormalizer()

	// Process valid results
	for _, result := range results.Results {
		// Get the original request
		req, exists := requestsByName[result.Name]
		if !exists {
			a.logger.Error("Got result for user not in batch",
				zap.String("username", result.Name))

			continue
		}

		// Find original info by username
		originalInfo, exists := originalInfos[result.Name]
		if !exists {
			a.logger.Warn("Original user info not found for result",
				zap.String("username", result.Name))

			invalidRequests[req.UserID] = req

			continue
		}

		// Get the translated info
		translatedInfo, exists := translatedInfos[result.Name]
		if !exists {
			a.logger.Warn("Translated user info not found",
				zap.String("username", result.Name))

			invalidRequests[req.UserID] = req

			continue
		}

		// Skip results with no content
		if result.Reason == "" || len(result.FlaggedContent) == 0 {
			a.logger.Debug("Reason analysis returned empty results",
				zap.String("username", result.Name))

			invalidRequests[req.UserID] = req

			continue
		}

		// Process flagged content to handle newlines
		processedContent := utils.SplitLines(result.FlaggedContent)

		// Validate flagged content against user texts
		isValid := normalizer.ValidateWords(processedContent,
			translatedInfo.Name,
			translatedInfo.DisplayName,
			translatedInfo.Description)

		// If the flagged content is valid, update the reasons map
		if isValid {
			mu.Lock()

			if _, exists := reasonsMap[originalInfo.ID]; !exists {
				reasonsMap[originalInfo.ID] = make(types.Reasons[enum.UserReasonType])
			}

			reasonsMap[originalInfo.ID].Add(enum.UserReasonTypeProfile, &types.Reason{
				Message:    result.Reason,
				Confidence: req.Confidence,
				Evidence:   processedContent,
			})
			mu.Unlock()

			truncatedReason := result.Reason
			if len(result.Reason) > 100 {
				truncatedReason = result.Reason[:100] + "..."
			}

			a.logger.Debug("Added user reason",
				zap.Int64("userID", originalInfo.ID),
				zap.String("username", result.Name),
				zap.Float64("confidence", req.Confidence),
				zap.String("reason_snippet", truncatedReason))
		} else {
			a.logger.Warn("Reason analysis flagged content did not pass validation",
				zap.Int64("userID", originalInfo.ID),
				zap.String("username", result.Name),
				zap.String("description", translatedInfo.Description),
				zap.Strings("flaggedContent", processedContent))

			invalidRequests[req.UserID] = req
		}
	}

	return invalidRequests
}

package ai

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/alpkeskin/gotoon"
	"github.com/bytedance/sonic"
	"github.com/openai/openai-go"
	"github.com/robalyx/rotector/internal/ai/client"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/database/types/enum"
	"github.com/robalyx/rotector/internal/setup"
	"github.com/robalyx/rotector/pkg/utils"
	"go.uber.org/zap"
	"golang.org/x/sync/semaphore"
)

const (
	// MaxOutfitThemes is the maximum number of outfit themes to include in analysis.
	MaxOutfitThemes = 10
	// OutfitReasonMaxRetries is the maximum number of retry attempts for outfit reason analysis.
	OutfitReasonMaxRetries = 3
)

// OutfitThemeSummary contains a summary of a detected outfit theme.
type OutfitThemeSummary struct {
	OutfitName string `json:"outfitName" jsonschema:"required,minLength=1,description=Name of the outfit with a detected theme"`
	Theme      string `json:"theme"      jsonschema:"required,minLength=1,description=Description of the specific theme detected"`
}

// UserOutfitData represents the data for a user's outfit violations.
type UserOutfitData struct {
	Username string               `json:"username" jsonschema:"required,minLength=1,description=Username of the account being analyzed"`
	Themes   []OutfitThemeSummary `json:"themes"   jsonschema:"required,maxItems=20,description=List of outfit themes detected for this user"`
}

// UserOutfitRequest contains the user data and outfit violations for analysis.
type UserOutfitRequest struct {
	UserInfo *types.ReviewUser `json:"-"`        // User info stored for internal reference, not sent to AI
	UserData UserOutfitData    `json:"userData"` // Outfit violation data to be analyzed
}

// OutfitAnalysis contains the result of analyzing a user's outfit violations.
type OutfitAnalysis struct {
	Name     string `json:"name"     jsonschema:"required,minLength=1,description=Username of the account being analyzed"`
	Analysis string `json:"analysis" jsonschema:"required,minLength=1,description=Analysis of outfit violation patterns for this user"`
}

// BatchOutfitAnalysis contains results for multiple users' outfit violations.
type BatchOutfitAnalysis struct {
	Results []OutfitAnalysis `json:"results" jsonschema:"required,maxItems=50,description=Array of outfit violation analyses for each user"`
}

// OutfitReasonAnalyzer handles AI-based analysis of outfit violations using OpenAI models.
type OutfitReasonAnalyzer struct {
	chat          client.ChatCompletions
	analysisSem   *semaphore.Weighted
	logger        *zap.Logger
	textLogger    *zap.Logger
	textDir       string
	model         string
	fallbackModel string
	batchSize     int
}

// OutfitReasonAnalysisSchema is the JSON schema for the outfit analysis response.
var OutfitReasonAnalysisSchema = utils.GenerateSchema[BatchOutfitAnalysis]()

// NewOutfitReasonAnalyzer creates an OutfitReasonAnalyzer.
func NewOutfitReasonAnalyzer(app *setup.App, logger *zap.Logger) *OutfitReasonAnalyzer {
	// Get text logger
	textLogger, textDir, err := app.LogManager.GetTextLogger("outfit_reason_analyzer")
	if err != nil {
		logger.Error("Failed to create text logger", zap.Error(err))
		textLogger = logger
	}

	return &OutfitReasonAnalyzer{
		chat:          app.AIClient.Chat(),
		analysisSem:   semaphore.NewWeighted(int64(app.Config.Worker.BatchSizes.OutfitReasonAnalysis)),
		logger:        logger.Named("ai_outfit_reason"),
		textLogger:    textLogger,
		textDir:       textDir,
		model:         app.Config.Common.OpenAI.OutfitReasonModel,
		fallbackModel: app.Config.Common.OpenAI.OutfitReasonFallbackModel,
		batchSize:     app.Config.Worker.BatchSizes.OutfitReasonAnalysisBatch,
	}
}

// ProcessFlaggedUsers processes users flagged by outfit analysis and generates detailed reasons.
func (a *OutfitReasonAnalyzer) ProcessFlaggedUsers(
	ctx context.Context, userInfos []*types.ReviewUser, reasonsMap map[int64]types.Reasons[enum.UserReasonType],
) {
	// Filter users to only those flagged for outfit violations and populate their Reasons
	var outfitFlaggedUsers []*types.ReviewUser

	for _, userInfo := range userInfos {
		if reasons, ok := reasonsMap[userInfo.ID]; ok {
			if reasons[enum.UserReasonTypeOutfit] != nil {
				// Create a copy with populated reasons for analysis
				userCopy := *userInfo
				userCopy.Reasons = reasons
				outfitFlaggedUsers = append(outfitFlaggedUsers, &userCopy)
			}
		}
	}

	if len(outfitFlaggedUsers) == 0 {
		return
	}

	// Generate detailed outfit reasons
	outfitReasons := a.GenerateOutfitReasons(ctx, outfitFlaggedUsers)

	// Update reasons with detailed outfit analysis
	for userID, analysis := range outfitReasons {
		if reasons, exists := reasonsMap[userID]; exists && reasons[enum.UserReasonTypeOutfit] != nil {
			// Update the message with the detailed analysis
			reasons[enum.UserReasonTypeOutfit].Message = analysis
		}
	}

	a.logger.Info("Completed outfit reason analysis", zap.Int("outfitFlaggedUsers", len(outfitFlaggedUsers)))
}

// GenerateOutfitReasons generates outfit violation analysis reasons for multiple users using the OpenAI model.
func (a *OutfitReasonAnalyzer) GenerateOutfitReasons(
	ctx context.Context, userInfos []*types.ReviewUser,
) map[int64]string {
	// Create outfit requests map for users with outfit violations
	outfitRequests := make(map[int64]UserOutfitRequest)

	for _, userInfo := range userInfos {
		// Get outfit reason from user's populated Reasons field
		outfitReason := userInfo.Reasons[enum.UserReasonTypeOutfit]
		if len(outfitReason.Evidence) == 0 {
			continue
		}

		// Parse outfit evidence: format is "{outfitName}|{theme}|{confidence}"
		var outfitThemes []OutfitThemeSummary

		for _, evidence := range outfitReason.Evidence {
			parts := strings.Split(evidence, "|")
			if len(parts) == 3 {
				outfitThemes = append(outfitThemes, OutfitThemeSummary{
					OutfitName: parts[0],
					Theme:      parts[1],
				})

				// Limit number of themes
				if len(outfitThemes) >= MaxOutfitThemes {
					break
				}
			}
		}

		// Skip if no valid themes found
		if len(outfitThemes) == 0 {
			continue
		}

		outfitRequests[userInfo.ID] = UserOutfitRequest{
			UserInfo: userInfo,
			UserData: UserOutfitData{
				Username: userInfo.Name,
				Themes:   outfitThemes,
			},
		}
	}

	// Process outfit requests
	results := make(map[int64]string)
	a.ProcessOutfitRequests(ctx, outfitRequests, results, 0)

	return results
}

// ProcessOutfitRequests processes outfit analysis requests with retry logic for invalid users.
func (a *OutfitReasonAnalyzer) ProcessOutfitRequests(
	ctx context.Context, outfitRequests map[int64]UserOutfitRequest, results map[int64]string, retryCount int,
) {
	if len(outfitRequests) == 0 {
		return
	}

	// Prevent infinite retries
	if retryCount > OutfitReasonMaxRetries {
		a.logger.Warn("Maximum retries reached for outfit analysis, skipping remaining users",
			zap.Int("retryCount", retryCount),
			zap.Int("maxRetries", OutfitReasonMaxRetries),
			zap.Int("remainingUsers", len(outfitRequests)))

		return
	}

	// Convert map to slice for batch processing
	requestSlice := make([]UserOutfitRequest, 0, len(outfitRequests))
	for _, req := range outfitRequests {
		requestSlice = append(requestSlice, req)
	}

	// Process batches with retry and splitting
	var (
		mu              sync.Mutex
		invalidMu       sync.Mutex
		invalidRequests = make(map[int64]UserOutfitRequest)
	)

	minBatchSize := max(len(requestSlice)/4, 1)

	err := utils.WithRetrySplitBatch(
		ctx, requestSlice, a.batchSize, minBatchSize, utils.GetAIRetryOptions(),
		func(batch []UserOutfitRequest) error {
			// Process the batch
			batchResults, err := a.processOutfitBatch(ctx, batch)
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
		func(batch []UserOutfitRequest) {
			// Log blocked content
			usernames := make([]string, len(batch))
			for i, req := range batch {
				usernames[i] = req.UserData.Username
			}

			// Log details of the blocked content
			a.textLogger.Warn("Content blocked in outfit analysis batch",
				zap.Strings("usernames", usernames),
				zap.Int("batch_size", len(batch)),
				zap.Any("requests", batch))

			// Save blocked outfit data to file
			filename := fmt.Sprintf("outfits_%s.txt", time.Now().Format("20060102_150405"))
			filePath := filepath.Join(a.textDir, filename)

			var buf bytes.Buffer
			for _, req := range batch {
				buf.WriteString(fmt.Sprintf("Username: %s\nOutfit Themes:\n", req.UserData.Username))

				for _, theme := range req.UserData.Themes {
					buf.WriteString(fmt.Sprintf("  - Outfit: %s\n    Theme: %s\n",
						theme.OutfitName, theme.Theme))
				}

				buf.WriteString("\n")
			}

			if err := os.WriteFile(filePath, buf.Bytes(), 0o600); err != nil {
				a.textLogger.Error("Failed to save blocked outfit data",
					zap.Error(err),
					zap.String("path", filePath))

				return
			}

			a.textLogger.Info("Saved blocked outfit data",
				zap.String("path", filePath))
		},
	)
	if err != nil {
		a.logger.Error("Error processing outfit requests", zap.Error(err))
	}

	// Process invalid requests if any
	if len(invalidRequests) > 0 {
		a.logger.Info("Retrying analysis for invalid results",
			zap.Int("invalidUsers", len(invalidRequests)),
			zap.Int("retryCount", retryCount))

		a.ProcessOutfitRequests(ctx, invalidRequests, results, retryCount+1)
	}

	a.logger.Info("Finished processing outfit requests",
		zap.Int("totalRequests", len(outfitRequests)),
		zap.Int("retriedUsers", len(invalidRequests)),
		zap.Int("retryCount", retryCount))
}

// processOutfitBatch handles the AI analysis for a batch of outfit data.
func (a *OutfitReasonAnalyzer) processOutfitBatch(ctx context.Context, batch []UserOutfitRequest) (*BatchOutfitAnalysis, error) {
	// Extract UserOutfitData for AI request
	batchData := make([]UserOutfitData, len(batch))
	for i, req := range batch {
		batchData[i] = req.UserData
	}

	// Convert to TOON format
	toonData, err := gotoon.Encode(batchData)
	if err != nil {
		return nil, fmt.Errorf("TOON marshal error: %w", err)
	}

	// Configure prompt for outfit analysis
	prompt := fmt.Sprintf(OutfitReasonUserPrompt, toonData)

	// Prepare chat completion parameters
	params := openai.ChatCompletionNewParams{
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(OutfitReasonSystemPrompt),
			openai.UserMessage(prompt),
		},
		ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONSchema: &openai.ResponseFormatJSONSchemaParam{
				JSONSchema: openai.ResponseFormatJSONSchemaJSONSchemaParam{
					Name:        "outfitAnalysis",
					Description: openai.String("Analysis of outfit violation patterns"),
					Schema:      OutfitReasonAnalysisSchema,
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
	var result BatchOutfitAnalysis

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
			a.logger.Debug("AI outfit analysis thought process",
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
		processedResults := make([]OutfitAnalysis, 0, len(result.Results))

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
func (a *OutfitReasonAnalyzer) processResults(
	results *BatchOutfitAnalysis, batch []UserOutfitRequest, finalResults map[int64]string, mu *sync.Mutex,
) map[int64]UserOutfitRequest {
	// Create map for retry requests
	invalidRequests := make(map[int64]UserOutfitRequest)

	// If no results returned, mark all users for retry
	if results == nil || len(results.Results) == 0 {
		a.logger.Warn("No results returned from outfit analysis, retrying all users")

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
	requestsByName := make(map[string]UserOutfitRequest, len(batch))
	for _, req := range batch {
		requestsByName[req.UserData.Username] = req
	}

	// Handle missing users
	for _, req := range batch {
		if _, wasProcessed := processedUsers[req.UserData.Username]; !wasProcessed {
			a.logger.Warn("User missing from outfit analysis results",
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
			a.logger.Debug("Outfit analysis returned empty results",
				zap.String("username", result.Name))

			invalidRequests[req.UserInfo.ID] = req

			continue
		}

		// Store valid result
		mu.Lock()

		finalResults[req.UserInfo.ID] = result.Analysis

		mu.Unlock()

		a.logger.Debug("Added outfit analysis result",
			zap.String("username", result.Name))
	}

	return invalidRequests
}

package ai

import (
	"context"
	"fmt"
	"maps"
	"sync"

	"github.com/bytedance/sonic"
	"github.com/openai/openai-go"
	"github.com/robalyx/rotector/internal/ai/client"
	"github.com/robalyx/rotector/internal/setup"
	"github.com/robalyx/rotector/internal/setup/config"
	"github.com/robalyx/rotector/pkg/utils"
	"github.com/sourcegraph/conc/pool"
	"github.com/tdewolff/minify/v2"
	"github.com/tdewolff/minify/v2/json"
	"go.uber.org/zap"
	"golang.org/x/sync/semaphore"
)

const (
	// WordlistAnalyzerMaxRetries is the maximum number of retry attempts for wordlist validation analysis.
	WordlistAnalyzerMaxRetries = 2
)

// WordlistAnalysisRequest represents a user's profile data with context for analysis.
type WordlistAnalysisRequest struct {
	Name              string                 `json:"name"`
	DisplayName       string                 `json:"displayName"`
	Description       string                 `json:"description"`
	UserReasonRequest *UserReasonRequest     `json:"userReasonRequest"`
	FlaggedMatches    []config.WordlistMatch `json:"flaggedMatches"`
}

// WordlistAnalysisResult represents the analysis result for a single user.
type WordlistAnalysisResult struct {
	Name    string `json:"name"    jsonschema_description:"Username"`
	Flagged bool   `json:"flagged" jsonschema_description:"Whether this user should be flagged based on analysis"`
}

// WordlistAnalysisResponse represents the AI's response for wordlist analysis.
type WordlistAnalysisResponse struct {
	Results []WordlistAnalysisResult `json:"results" jsonschema_description:"List of analysis results for each user"`
}

// WordlistAnalyzer handles AI-based validation of wordlist matches to reduce false positives.
type WordlistAnalyzer struct {
	chat        client.ChatCompletions
	minify      *minify.M
	analysisSem *semaphore.Weighted
	logger      *zap.Logger
	model       string
	batchSize   int
}

// WordlistAnalysisSchema is the JSON schema for the wordlist analysis response.
var WordlistAnalysisSchema = utils.GenerateSchema[WordlistAnalysisResponse]()

// NewWordlistAnalyzer creates a new WordlistAnalyzer.
func NewWordlistAnalyzer(app *setup.App, logger *zap.Logger) *WordlistAnalyzer {
	m := minify.New()
	m.AddFunc("application/json", json.Minify)

	return &WordlistAnalyzer{
		chat:        app.AIClient.Chat(),
		minify:      m,
		analysisSem: semaphore.NewWeighted(int64(app.Config.Worker.BatchSizes.WordlistAnalysis)),
		logger:      logger.Named("ai_wordlist"),
		model:       app.Config.Common.OpenAI.WordlistModel,
		batchSize:   app.Config.Worker.BatchSizes.WordlistAnalysisBatch,
	}
}

// AnalyzeUsers validates flagging decisions using AI to determine if users should remain flagged.
func (a *WordlistAnalyzer) AnalyzeUsers(
	ctx context.Context, requests []WordlistAnalysisRequest, retryCount int,
) map[string]struct{} {
	if len(requests) == 0 {
		return make(map[string]struct{})
	}

	// Stop retrying if we've exceeded the maximum retry count
	if retryCount > WordlistAnalyzerMaxRetries {
		a.logger.Warn("Exceeded maximum retries for wordlist analysis",
			zap.Int("retryCount", retryCount),
			zap.Int("remainingRequests", len(requests)))

		return make(map[string]struct{})
	}

	results := make(map[string]struct{})
	invalidRequests := make(map[string]WordlistAnalysisRequest)
	numBatches := (len(requests) + a.batchSize - 1) / a.batchSize

	var (
		p         = pool.New().WithContext(ctx)
		mu        sync.Mutex
		invalidMu sync.Mutex
	)

	for i := range numBatches {
		start := i * a.batchSize
		end := min(start+a.batchSize, len(requests))
		batch := requests[start:end]

		p.Go(func(ctx context.Context) error {
			// Acquire semaphore
			if err := a.analysisSem.Acquire(ctx, 1); err != nil {
				return fmt.Errorf("failed to acquire semaphore: %w", err)
			}
			defer a.analysisSem.Release(1)

			// Analyze batch
			batchResults, invalidBatch := a.processBatchWithAnalysis(ctx, batch)

			// Merge valid results
			mu.Lock()

			for username := range batchResults {
				results[username] = struct{}{}
			}

			mu.Unlock()

			// Merge invalid requests
			if len(invalidBatch) > 0 {
				invalidMu.Lock()
				maps.Copy(invalidRequests, invalidBatch)
				invalidMu.Unlock()
			}

			return nil
		})
	}

	// Wait for all batches to complete
	if err := p.Wait(); err != nil {
		a.logger.Error("Failed to process all wordlist validation batches", zap.Error(err))
		return results
	}

	a.logger.Info("Completed wordlist analysis batch processing",
		zap.Int("totalRequests", len(requests)),
		zap.Int("validResults", len(results)),
		zap.Int("invalidRequests", len(invalidRequests)),
		zap.Int("retryCount", retryCount))

	// Process invalid requests if any
	if len(invalidRequests) > 0 {
		a.logger.Info("Retrying wordlist analysis for invalid results",
			zap.Int("invalidRequests", len(invalidRequests)),
			zap.Int("retryCount", retryCount))

		// Convert map to slice for retry
		retryRequests := make([]WordlistAnalysisRequest, 0, len(invalidRequests))
		for _, req := range invalidRequests {
			retryRequests = append(retryRequests, req)
		}

		// Recursively process invalid requests
		retryResults := a.AnalyzeUsers(ctx, retryRequests, retryCount+1)

		// Merge retry results
		for username := range retryResults {
			results[username] = struct{}{}
		}
	}

	return results
}

// processBatchWithAnalysis handles the AI analysis for a batch of wordlist analysis requests with validation.
func (a *WordlistAnalyzer) processBatchWithAnalysis(
	ctx context.Context, batch []WordlistAnalysisRequest,
) (map[string]struct{}, map[string]WordlistAnalysisRequest) {
	results := make(map[string]struct{})
	invalidRequests := make(map[string]WordlistAnalysisRequest)

	// Create request map for validation lookup
	requestMap := make(map[string]WordlistAnalysisRequest)
	for _, req := range batch {
		requestMap[req.Name] = req
	}

	// Process batch
	aiResults, processedUsers, err := a.processBatch(ctx, batch)
	if err != nil {
		for _, req := range batch {
			invalidRequests[req.Name] = req
		}

		return results, invalidRequests
	}

	// Process each AI result
	for username := range aiResults {
		if _, exists := requestMap[username]; !exists {
			a.logger.Warn("AI returned result for unknown username", zap.String("username", username))
			continue
		}

		// Store the flagging decision
		results[username] = struct{}{}
		a.logger.Debug("Processed wordlist analysis result",
			zap.String("username", username),
			zap.Bool("flagged", true))
	}

	// Check for missing users and add them for retry
	for _, req := range batch {
		if _, processed := processedUsers[req.Name]; !processed {
			a.logger.Warn("AI did not return result for user, marking as invalid",
				zap.String("username", req.Name))
			invalidRequests[req.Name] = req
		}
	}

	return results, invalidRequests
}

// processBatch handles the AI analysis for a batch of wordlist analysis requests.
func (a *WordlistAnalyzer) processBatch(
	ctx context.Context, batch []WordlistAnalysisRequest,
) (map[string]struct{}, map[string]struct{}, error) {
	results := make(map[string]struct{})

	// Convert to JSON
	requestJSON, err := sonic.Marshal(batch)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w", utils.ErrJSONProcessing, err)
	}

	// Minify JSON to reduce token usage
	requestJSON, err = a.minify.Bytes("application/json", requestJSON)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w", utils.ErrJSONProcessing, err)
	}

	// Prepare request prompt with validation data
	requestPrompt := WordlistRequestPrompt + string(requestJSON)

	// Prepare chat completion parameters
	params := openai.ChatCompletionNewParams{
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(WordlistSystemPrompt),
			openai.UserMessage(requestPrompt),
		},
		ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONSchema: &openai.ResponseFormatJSONSchemaParam{
				JSONSchema: openai.ResponseFormatJSONSchemaJSONSchemaParam{
					Name:        "wordlistAnalysis",
					Description: openai.String("Analyze user profiles to validate flagging decisions"),
					Schema:      WordlistAnalysisSchema,
					Strict:      openai.Bool(true),
				},
			},
		},
		Model:       a.model,
		Temperature: openai.Float(0.0),
		TopP:        openai.Float(0.2),
	}

	var response WordlistAnalysisResponse

	err = a.chat.NewWithRetry(ctx, params, func(resp *openai.ChatCompletion, err error) error {
		// Handle API error
		if err != nil {
			return fmt.Errorf("openai API error: %w", err)
		}

		// Check for empty response
		if len(resp.Choices) == 0 || len(resp.Choices[0].Message.Content) == 0 {
			return fmt.Errorf("%w: no response from model", utils.ErrModelResponse)
		}

		// Parse response from AI
		if err := sonic.Unmarshal([]byte(resp.Choices[0].Message.Content), &response); err != nil {
			return fmt.Errorf("JSON unmarshal error: %w", err)
		}

		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	// Process results and store only flagged users
	allProcessed := make(map[string]struct{})
	for _, result := range response.Results {
		allProcessed[result.Name] = struct{}{}
		if result.Flagged {
			results[result.Name] = struct{}{}
		}
	}

	a.logger.Debug("Processed wordlist analysis batch",
		zap.Int("batchSize", len(batch)),
		zap.Int("totalResults", len(allProcessed)),
		zap.Int("flaggedUsers", len(results)))

	return results, allProcessed, nil
}

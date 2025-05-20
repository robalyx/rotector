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
	"github.com/robalyx/rotector/internal/database/types/enum"
	"github.com/robalyx/rotector/internal/setup"
	"github.com/robalyx/rotector/internal/translator"
	"github.com/robalyx/rotector/pkg/utils"
	"github.com/sourcegraph/conc/pool"
	"github.com/tdewolff/minify/v2"
	"github.com/tdewolff/minify/v2/json"
	"go.uber.org/zap"
	"golang.org/x/sync/semaphore"
)

// UserSummary is a struct for user summaries for AI analysis.
type UserSummary struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName,omitempty"`
	Description string `json:"description"`
}

// FlaggedUsers holds a list of users that the AI has identified as inappropriate.
// The JSON schema is used to ensure consistent responses from the AI.
type FlaggedUsers struct {
	Users []FlaggedUser `json:"users" jsonschema_description:"List of users that have been flagged for inappropriate content"`
}

// FlaggedUser contains the AI's analysis results for a single user.
// The confidence score and flagged content help moderators make decisions.
type FlaggedUser struct {
	Name           string   `json:"name"           jsonschema_description:"Username of the flagged account"`
	Reason         string   `json:"reason"         jsonschema_description:"Clear explanation of why the user was flagged"`
	FlaggedContent []string `json:"flaggedContent" jsonschema_description:"List of exact quotes from the user's content that were flagged"`
	Confidence     float64  `json:"confidence"     jsonschema_description:"Overall confidence score for the violations (0.0-1.0)"`
	HasSocials     bool     `json:"hasSocials"     jsonschema_description:"Whether the user's description contains social media"`
}

// UserAnalyzer handles AI-based content analysis using OpenAI models.
type UserAnalyzer struct {
	chat        client.ChatCompletions
	minify      *minify.M
	translator  *translator.Translator
	analysisSem *semaphore.Weighted
	logger      *zap.Logger
	textLogger  *zap.Logger
	textDir     string
	model       string
	batchSize   int
}

// UserAnalysisSchema is the JSON schema for the user analysis response.
var UserAnalysisSchema = utils.GenerateSchema[FlaggedUsers]()

// NewUserAnalyzer creates an UserAnalyzer with separate models for user and friend analysis.
func NewUserAnalyzer(app *setup.App, translator *translator.Translator, logger *zap.Logger) *UserAnalyzer {
	// Create a minifier for JSON optimization
	m := minify.New()
	m.AddFunc(ApplicationJSON, json.Minify)

	// Get text logger
	textLogger, textDir, err := app.LogManager.GetTextLogger("user_analyzer")
	if err != nil {
		logger.Error("Failed to create text logger", zap.Error(err))
		textLogger = logger
	}

	return &UserAnalyzer{
		chat:        app.AIClient.Chat(),
		minify:      m,
		translator:  translator,
		analysisSem: semaphore.NewWeighted(int64(app.Config.Worker.BatchSizes.UserAnalysis)),
		logger:      logger.Named("ai_user"),
		textLogger:  textLogger,
		textDir:     textDir,
		model:       app.Config.Common.OpenAI.UserModel,
		batchSize:   app.Config.Worker.BatchSizes.UserAnalysisBatch,
	}
}

// ProcessUsers analyzes user content for a batch of users.
func (a *UserAnalyzer) ProcessUsers(users []*types.ReviewUser, reasonsMap map[uint64]types.Reasons[enum.UserReasonType]) {
	// Track counts before processing
	existingFlags := len(reasonsMap)
	numBatches := (len(users) + a.batchSize - 1) / a.batchSize

	// Process batches concurrently
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	var (
		p  = pool.New().WithContext(ctx)
		mu sync.Mutex
	)

	for i := range numBatches {
		start := i * a.batchSize
		end := min(start+a.batchSize, len(users))

		infoBatch := users[start:end]
		p.Go(func(ctx context.Context) error {
			// Acquire semaphore
			if err := a.analysisSem.Acquire(ctx, 1); err != nil {
				return fmt.Errorf("failed to acquire semaphore: %w", err)
			}
			defer a.analysisSem.Release(1)

			// Process batch
			if err := a.processBatch(ctx, infoBatch, reasonsMap, &mu); err != nil {
				a.logger.Error("Failed to process batch",
					zap.Error(err),
					zap.Int("batchStart", start),
					zap.Int("batchEnd", end))
				return err
			}
			return nil
		})
	}

	// Wait for all batches to complete
	if err := p.Wait(); err != nil {
		return
	}

	a.logger.Info("Received AI user analysis",
		zap.Int("totalUsers", len(users)),
		zap.Int("newFlags", len(reasonsMap)-existingFlags))
}

// processUserBatch handles the AI analysis for a batch of user summaries.
func (a *UserAnalyzer) processUserBatch(ctx context.Context, batch []UserSummary) (*FlaggedUsers, error) {
	// Convert to JSON
	userInfoJSON, err := sonic.Marshal(batch)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", utils.ErrJSONProcessing, err)
	}

	// Minify JSON to reduce token usage
	userInfoJSON, err = a.minify.Bytes(ApplicationJSON, userInfoJSON)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", utils.ErrJSONProcessing, err)
	}

	// Prepare request prompt with user info
	requestPrompt := UserRequestPrompt + string(userInfoJSON)

	// Prepare chat completion parameters
	params := openai.ChatCompletionNewParams{
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(UserSystemPrompt),
			openai.UserMessage(requestPrompt),
		},
		ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONSchema: &openai.ResponseFormatJSONSchemaParam{
				JSONSchema: openai.ResponseFormatJSONSchemaJSONSchemaParam{
					Name:        "userAnalysis",
					Description: openai.String("Analysis of user content"),
					Schema:      UserAnalysisSchema,
					Strict:      openai.Bool(true),
				},
			},
		},
		Model:       a.model,
		Temperature: openai.Float(0.1),
		TopP:        openai.Float(0.2),
	}

	params = client.WithReasoning(params, client.ReasoningOptions{
		Effort:    openai.ReasoningEffortHigh,
		MaxTokens: 8192,
		Exclude:   false,
	})

	// Make API request
	var result FlaggedUsers
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
			a.logger.Debug("AI user analysis thought process",
				zap.String("model", resp.Model),
				zap.String("thought", thought.Raw()))
		}

		// Parse response from AI
		if err := sonic.Unmarshal([]byte(message.Content), &result); err != nil {
			return fmt.Errorf("JSON unmarshal error: %w", err)
		}

		return nil
	})

	return &result, err
}

// processBatch handles analysis for a batch of users.
func (a *UserAnalyzer) processBatch(
	ctx context.Context, userInfos []*types.ReviewUser, reasonsMap map[uint64]types.Reasons[enum.UserReasonType], mu *sync.Mutex,
) error {
	// Translate all descriptions concurrently
	translatedInfos, originalInfos := a.prepareUserInfos(ctx, userInfos)

	// Convert map to slice for AI request
	userInfosWithoutID := make([]UserSummary, 0, len(translatedInfos))
	for _, userInfo := range translatedInfos {
		summary := UserSummary{
			Name: userInfo.Name,
		}

		// Only include display name if it's different from the username
		if userInfo.DisplayName != userInfo.Name {
			summary.DisplayName = userInfo.DisplayName
		}

		// Replace empty descriptions with placeholder
		description := userInfo.Description
		if description == "" {
			description = "No description"
		}
		summary.Description = description

		userInfosWithoutID = append(userInfosWithoutID, summary)
	}

	// Create operation function for batch processing
	minBatchSize := max(len(userInfosWithoutID)/4, 1)

	var result *FlaggedUsers
	err := utils.WithRetrySplitBatch(
		ctx, userInfosWithoutID, len(userInfosWithoutID), minBatchSize, utils.GetAIRetryOptions(),
		func(batch []UserSummary) error {
			var err error
			result, err = a.processUserBatch(ctx, batch)
			return err
		},
		func(batch []UserSummary) {
			usernames := make([]string, len(batch))
			for i, user := range batch {
				usernames[i] = user.Name
			}

			// Log detailed content to text logger
			a.textLogger.Warn("Content blocked in user analysis batch",
				zap.Strings("usernames", usernames),
				zap.Int("batch_size", len(batch)),
				zap.Any("users", batch))

			// Save blocked user data to file
			filename := fmt.Sprintf("users_%s.txt", time.Now().Format("20060102_150405"))
			filepath := filepath.Join(a.textDir, filename)

			var buf bytes.Buffer
			for _, user := range batch {
				buf.WriteString(fmt.Sprintf("Username: %s\n", user.Name))
				if user.DisplayName != "" && user.DisplayName != user.Name {
					buf.WriteString(fmt.Sprintf("Display Name: %s\n", user.DisplayName))
				}
				buf.WriteString(fmt.Sprintf("Description: %s\n\n", user.Description))
			}

			if err := os.WriteFile(filepath, buf.Bytes(), 0o600); err != nil {
				a.textLogger.Error("Failed to save blocked user data",
					zap.Error(err),
					zap.String("path", filepath))
				return
			}

			a.textLogger.Info("Saved blocked user data",
				zap.String("path", filepath))
		},
	)
	if err != nil {
		return err
	}

	// Validate AI responses
	a.validateAndUpdateFlaggedUsers(result, translatedInfos, originalInfos, reasonsMap, mu)

	return nil
}

// validateAndUpdateFlaggedUsers validates the flagged users and updates the flaggedUsers map.
func (a *UserAnalyzer) validateAndUpdateFlaggedUsers(
	result *FlaggedUsers, translatedInfos, originalInfos map[string]*types.ReviewUser,
	reasonsMap map[uint64]types.Reasons[enum.UserReasonType], mu *sync.Mutex,
) {
	normalizer := utils.NewTextNormalizer()
	for _, flaggedUser := range result.Users {
		translatedInfo, exists := translatedInfos[flaggedUser.Name]
		originalInfo, hasOriginal := originalInfos[flaggedUser.Name]

		// Check if the flagged user exists in both maps
		if !exists && !hasOriginal {
			a.logger.Info("AI flagged non-existent user", zap.String("username", flaggedUser.Name))
			continue
		}

		// Check if the user has social media links
		mu.Lock()
		originalInfo.HasSocials = flaggedUser.HasSocials
		mu.Unlock()

		// Skip results with no violations
		if flaggedUser.Reason == "" || flaggedUser.Reason == "NO_VIOLATIONS" {
			continue
		}

		// Validate confidence level
		if flaggedUser.Confidence < 0.1 || flaggedUser.Confidence > 1.0 {
			a.logger.Debug("AI flagged user with invalid confidence",
				zap.String("username", flaggedUser.Name),
				zap.Float64("confidence", flaggedUser.Confidence))
			continue
		}

		// Validate that flagged content exists
		if len(flaggedUser.FlaggedContent) == 0 {
			a.logger.Debug("AI flagged user without specific content",
				zap.String("username", flaggedUser.Name))
			continue
		}

		// Process flagged content to handle newlines
		processedContent := utils.SplitLines(flaggedUser.FlaggedContent)

		// Validate flagged content against user texts
		isValid := normalizer.ValidateWords(processedContent,
			translatedInfo.Name,
			translatedInfo.DisplayName,
			translatedInfo.Description)

		// If the flagged user is valid, update the reasons map
		if isValid {
			mu.Lock()
			if _, exists := reasonsMap[originalInfo.ID]; !exists {
				reasonsMap[originalInfo.ID] = make(types.Reasons[enum.UserReasonType])
			}

			reasonsMap[originalInfo.ID].Add(enum.UserReasonTypeProfile, &types.Reason{
				Message:    flaggedUser.Reason,
				Confidence: flaggedUser.Confidence,
				Evidence:   processedContent,
			})
			mu.Unlock()
		} else {
			a.logger.Warn("AI flagged content did not pass validation",
				zap.Uint64("userID", originalInfo.ID),
				zap.String("flaggedUsername", flaggedUser.Name),
				zap.String("username", originalInfo.Name),
				zap.String("description", originalInfo.Description),
				zap.Strings("flaggedContent", processedContent))
		}
	}
}

// prepareUserInfos translates user descriptions for different languages and encodings.
// Returns maps of both translated and original user infos for validation.
func (a *UserAnalyzer) prepareUserInfos(
	ctx context.Context, userInfos []*types.ReviewUser,
) (map[string]*types.ReviewUser, map[string]*types.ReviewUser) {
	var (
		originalInfos   = make(map[string]*types.ReviewUser)
		translatedInfos = make(map[string]*types.ReviewUser)
		p               = pool.New().WithContext(ctx)
		mu              sync.Mutex
	)

	// Initialize maps and spawn translation goroutines
	for _, info := range userInfos {
		originalInfos[info.Name] = info

		p.Go(func(ctx context.Context) error {
			// Skip empty descriptions
			if info.Description == "" {
				mu.Lock()
				translatedInfos[info.Name] = info
				mu.Unlock()
				return nil
			}

			// Translate the description with retry
			var translated string
			err := utils.WithRetry(ctx, func() error {
				var err error
				translated, err = a.translator.Translate(
					ctx,
					info.Description,
					"auto", // Auto-detect source language
					"en",   // Translate to English
				)
				return err
			}, utils.GetAIRetryOptions())
			if err != nil {
				// Use original userInfo if translation fails
				mu.Lock()
				translatedInfos[info.Name] = info
				mu.Unlock()
				a.logger.Error("Translation failed, using original description",
					zap.String("username", info.Name),
					zap.Error(err))
				return nil
			}

			// Create new Info with translated description
			translatedInfo := *info
			if translatedInfo.Description != translated {
				translatedInfo.Description = translated
				a.logger.Debug("Translated description", zap.String("username", info.Name))
			}
			mu.Lock()
			translatedInfos[info.Name] = &translatedInfo
			mu.Unlock()
			return nil
		})
	}

	// Wait for all translations to complete
	if err := p.Wait(); err != nil {
		a.logger.Error("Error during translations", zap.Error(err))
	}

	return translatedInfos, originalInfos
}

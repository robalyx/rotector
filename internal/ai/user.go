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
type FlaggedUsers struct {
	Users []FlaggedUser `json:"users" jsonschema_description:"List of users that have been flagged for inappropriate content"`
}

// FlaggedUser contains the AI's analysis results for a single user.
type FlaggedUser struct {
	Name              string   `json:"name"                        jsonschema_description:"Username of the flagged account"`
	Hint              string   `json:"hint"                        jsonschema_description:"Brief clinical description using safe terminology"`
	Confidence        float64  `json:"confidence"                  jsonschema_description:"Overall confidence score for the violations"`
	HasSocials        bool     `json:"hasSocials"                  jsonschema_description:"Whether the user's description has social media"`
	ViolationLocation []string `json:"violationLocation,omitempty" jsonschema_description:"Locations of violations"`
	LanguagePattern   []string `json:"languagePattern,omitempty"   jsonschema_description:"Linguistic patterns detected"`
	LanguageUsed      []string `json:"languageUsed,omitempty"      jsonschema_description:"Languages or encodings detected in content"`
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

// NewUserAnalyzer creates an UserAnalyzer.
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
func (a *UserAnalyzer) ProcessUsers(
	ctx context.Context, users []*types.ReviewUser,
	translatedInfos map[string]*types.ReviewUser, originalInfos map[string]*types.ReviewUser,
) map[uint64]UserReasonRequest {
	userReasonRequests := make(map[uint64]UserReasonRequest)
	numBatches := (len(users) + a.batchSize - 1) / a.batchSize

	// Process batches concurrently
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
			if err := a.processBatch(ctx, infoBatch, translatedInfos, originalInfos, userReasonRequests, &mu); err != nil {
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
		return userReasonRequests
	}

	a.logger.Info("Completed initial user analysis",
		zap.Int("totalUsers", len(users)),
		zap.Int("flaggedUsers", len(userReasonRequests)))

	return userReasonRequests
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
		if thought := message.JSON.ExtraFields["reasoning"]; thought.Valid() {
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

// processBatch handles the AI analysis for a batch of user summaries.
func (a *UserAnalyzer) processBatch(
	ctx context.Context, userInfos []*types.ReviewUser, translatedInfos map[string]*types.ReviewUser,
	originalInfos map[string]*types.ReviewUser, userReasonRequests map[uint64]UserReasonRequest, mu *sync.Mutex,
) error {
	// Convert map to slice for AI request
	userInfosWithoutID := make([]UserSummary, 0, len(userInfos))
	for _, userInfo := range userInfos {
		// Get the translated info
		translatedInfo, exists := translatedInfos[userInfo.Name]
		if !exists {
			a.logger.Warn("Translated info not found for user",
				zap.String("username", userInfo.Name))
			translatedInfo = userInfo
		}

		summary := UserSummary{
			Name: translatedInfo.Name,
		}

		// Only include display name if it's different from the username
		if translatedInfo.DisplayName != translatedInfo.Name {
			summary.DisplayName = translatedInfo.DisplayName
		}

		// Replace empty descriptions with placeholder
		description := translatedInfo.Description
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

	// Process AI responses and create reason requests
	a.processAndCreateRequests(result, originalInfos, translatedInfos, userReasonRequests, mu)

	return nil
}

// processAndCreateRequests processes the AI responses and creates reason requests.
func (a *UserAnalyzer) processAndCreateRequests(
	result *FlaggedUsers, originalInfos map[string]*types.ReviewUser,
	translatedInfos map[string]*types.ReviewUser,
	userReasonRequests map[uint64]UserReasonRequest, mu *sync.Mutex,
) {
	for _, flaggedUser := range result.Users {
		originalInfo, hasOriginal := originalInfos[flaggedUser.Name]
		if !hasOriginal {
			a.logger.Info("AI flagged non-existent user", zap.String("username", flaggedUser.Name))
			continue
		}

		// Get the translated info
		translatedInfo, hasTranslated := translatedInfos[flaggedUser.Name]
		if !hasTranslated {
			a.logger.Warn("Translated info not found for flagged user, using original",
				zap.String("username", flaggedUser.Name))
			translatedInfo = originalInfo
		}

		// Set the HasSocials field
		mu.Lock()
		originalInfo.HasSocials = flaggedUser.HasSocials
		mu.Unlock()

		// Skip results with no violations
		if flaggedUser.Hint == "" || flaggedUser.Hint == "NO_VIOLATIONS" {
			continue
		}

		// Validate confidence level
		if flaggedUser.Confidence < 0.1 || flaggedUser.Confidence > 1.0 {
			a.logger.Debug("AI flagged user with invalid confidence",
				zap.String("username", flaggedUser.Name),
				zap.Float64("confidence", flaggedUser.Confidence))
			continue
		}

		// Create a user summary for the reason request
		summary := &UserSummary{
			Name: translatedInfo.Name,
		}

		// Only include display name if it's different from the username
		if translatedInfo.DisplayName != translatedInfo.Name {
			summary.DisplayName = translatedInfo.DisplayName
		}

		// Replace empty descriptions with placeholder
		description := translatedInfo.Description
		if description == "" {
			description = "No description"
		}
		summary.Description = description

		// Create and store the reason request
		mu.Lock()
		userReasonRequests[originalInfo.ID] = UserReasonRequest{
			User:              summary,
			Confidence:        flaggedUser.Confidence,
			Hint:              flaggedUser.Hint,
			ViolationLocation: flaggedUser.ViolationLocation,
			LanguagePattern:   flaggedUser.LanguagePattern,
			LanguageUsed:      flaggedUser.LanguageUsed,
			UserID:            originalInfo.ID,
		}
		mu.Unlock()

		a.logger.Debug("Created reason request for user",
			zap.String("username", flaggedUser.Name),
			zap.Float64("confidence", flaggedUser.Confidence),
			zap.String("hint", flaggedUser.Hint),
			zap.Strings("violationLocation", flaggedUser.ViolationLocation),
			zap.Strings("languagePattern", flaggedUser.LanguagePattern),
			zap.Strings("languageUsed", flaggedUser.LanguageUsed))
	}
}

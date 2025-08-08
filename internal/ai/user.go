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

// ProcessUsersParams contains all the parameters needed for user analysis processing.
type ProcessUsersParams struct {
	Users                     []*types.ReviewUser                          `json:"users"`
	TranslatedInfos           map[string]*types.ReviewUser                 `json:"translatedInfos"`
	OriginalInfos             map[string]*types.ReviewUser                 `json:"originalInfos"`
	ReasonsMap                map[int64]types.Reasons[enum.UserReasonType] `json:"reasonsMap"`
	ConfirmedFriendsMap       map[int64]map[int64]*types.ReviewUser        `json:"confirmedFriendsMap"`
	FlaggedFriendsMap         map[int64]map[int64]*types.ReviewUser        `json:"flaggedFriendsMap"`
	ConfirmedGroupsMap        map[int64]map[int64]*types.ReviewGroup       `json:"confirmedGroupsMap"`
	FlaggedGroupsMap          map[int64]map[int64]*types.ReviewGroup       `json:"flaggedGroupsMap"`
	InappropriateProfileFlags map[int64]struct{}                           `json:"inappropriateProfileFlags"`
	InappropriateFriendsFlags map[int64]struct{}                           `json:"inappropriateFriendsFlags"`
	InappropriateGroupsFlags  map[int64]struct{}                           `json:"inappropriateGroupsFlags"`
}

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
	chat               client.ChatCompletions
	minify             *minify.M
	translator         *translator.Translator
	userReasonAnalyzer *UserReasonAnalyzer
	analysisSem        *semaphore.Weighted
	logger             *zap.Logger
	textLogger         *zap.Logger
	textDir            string
	model              string
	batchSize          int
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
		chat:               app.AIClient.Chat(),
		minify:             m,
		translator:         translator,
		userReasonAnalyzer: NewUserReasonAnalyzer(app, logger),
		analysisSem:        semaphore.NewWeighted(int64(app.Config.Worker.BatchSizes.UserAnalysis)),
		logger:             logger.Named("ai_user"),
		textLogger:         textLogger,
		textDir:            textDir,
		model:              app.Config.Common.OpenAI.UserModel,
		batchSize:          app.Config.Worker.BatchSizes.UserAnalysisBatch,
	}
}

// ProcessUsers analyzes user content for a batch of users.
func (a *UserAnalyzer) ProcessUsers(ctx context.Context, params *ProcessUsersParams) {
	userReasonRequests := make(map[int64]UserReasonRequest)
	numBatches := (len(params.Users) + a.batchSize - 1) / a.batchSize

	// Process batches concurrently
	var (
		p  = pool.New().WithContext(ctx)
		mu sync.Mutex
	)

	for i := range numBatches {
		start := i * a.batchSize
		end := min(start+a.batchSize, len(params.Users))

		infoBatch := params.Users[start:end]

		p.Go(func(ctx context.Context) error {
			// Acquire semaphore
			if err := a.analysisSem.Acquire(ctx, 1); err != nil {
				return fmt.Errorf("failed to acquire semaphore: %w", err)
			}
			defer a.analysisSem.Release(1)

			// Process batch
			if err := a.processBatch(
				ctx, infoBatch, params, userReasonRequests, &mu,
			); err != nil {
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

	a.logger.Info("Completed initial user analysis",
		zap.Int("totalUsers", len(params.Users)),
		zap.Int("flaggedUsers", len(userReasonRequests)))

	// Generate detailed reasons for flagged users
	if len(userReasonRequests) > 0 {
		a.userReasonAnalyzer.ProcessFlaggedUsers(
			ctx, userReasonRequests, params.TranslatedInfos, params.OriginalInfos, params.ReasonsMap, 0,
		)
	}
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
		Temperature: openai.Float(0.0),
		TopP:        openai.Float(0.2),
	}
	params.SetExtraFields(map[string]any{
		"reasoning": map[string]any{
			"max_tokens": 0,
		},
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
	ctx context.Context, userInfos []*types.ReviewUser, params *ProcessUsersParams,
	userReasonRequests map[int64]UserReasonRequest, mu *sync.Mutex,
) error {
	// Convert map to slice for AI request
	userInfosWithoutID := make([]UserSummary, 0, len(userInfos))
	for _, userInfo := range userInfos {
		// Get the translated info
		translatedInfo, exists := params.TranslatedInfos[userInfo.Name]
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
			filePath := filepath.Join(a.textDir, filename)

			var buf bytes.Buffer
			for _, user := range batch {
				buf.WriteString(fmt.Sprintf("Username: %s\n", user.Name))

				if user.DisplayName != "" && user.DisplayName != user.Name {
					buf.WriteString(fmt.Sprintf("Display Name: %s\n", user.DisplayName))
				}

				buf.WriteString(fmt.Sprintf("Description: %s\n\n", user.Description))
			}

			if err := os.WriteFile(filePath, buf.Bytes(), 0o600); err != nil {
				a.textLogger.Error("Failed to save blocked user data",
					zap.Error(err),
					zap.String("path", filePath))

				return
			}

			a.textLogger.Info("Saved blocked user data",
				zap.String("path", filePath))
		},
	)
	if err != nil {
		return err
	}

	// Process AI responses and create reason requests
	a.processAndCreateRequests(
		result, params, userReasonRequests, mu,
	)

	return nil
}

// shouldSkipFlaggedUser determines if a flagged user should be skipped based on various conditions.
func (a *UserAnalyzer) shouldSkipFlaggedUser(
	flaggedUser FlaggedUser, originalInfo *types.ReviewUser, params *ProcessUsersParams,
) bool {
	// Skip results with no violations
	if flaggedUser.Hint == "" || flaggedUser.Hint == "NO_VIOLATIONS" {
		return true
	}

	// Validate confidence level
	if flaggedUser.Confidence < 0.1 || flaggedUser.Confidence > 1.0 {
		a.logger.Debug("AI flagged user with invalid confidence",
			zap.String("username", flaggedUser.Name),
			zap.Float64("confidence", flaggedUser.Confidence))

		return true
	}

	// Skip extra checks if user is flagged with inappropriate profile
	if _, exists := params.InappropriateProfileFlags[originalInfo.ID]; exists {
		return false
	}

	// Special case for users with less than 1.0 confidence and more than 15 friends
	if flaggedUser.Confidence < 1.0 {
		totalFriends := len(originalInfo.Friends)
		if totalFriends > 15 {
			confirmedFriends := params.ConfirmedFriendsMap[originalInfo.ID]
			flaggedFriends := params.FlaggedFriendsMap[originalInfo.ID]
			totalInappropriateFriends := len(confirmedFriends) + len(flaggedFriends)

			if totalInappropriateFriends < 1 {
				a.logger.Info("Skipping user with >15 friends, confidence <1.0, but no inappropriate friends",
					zap.String("username", flaggedUser.Name),
					zap.Float64("confidence", flaggedUser.Confidence),
					zap.Int("totalFriendsCount", totalFriends),
					zap.Int("inappropriateFriendsCount", totalInappropriateFriends))

				return true
			}
		}
	}

	// Skip users with specific conditions when they have no existing reasons
	if len(params.ReasonsMap[originalInfo.ID]) == 0 {
		if flaggedUser.Confidence < 0.8 {
			a.logger.Info("Skipping user with low confidence and no existing reasons",
				zap.String("username", flaggedUser.Name),
				zap.Float64("confidence", flaggedUser.Confidence))

			return true
		}

		if flaggedUser.Confidence < 0.9 {
			// Check friends
			confirmedFriends := params.ConfirmedFriendsMap[originalInfo.ID]
			flaggedFriends := params.FlaggedFriendsMap[originalInfo.ID]
			totalInappropriateFriends := len(confirmedFriends) + len(flaggedFriends)
			totalFriends := len(originalInfo.Friends)
			hasSufficientInappropriateFriends := totalInappropriateFriends >= 5 ||
				(totalFriends > 0 && float64(totalInappropriateFriends)/float64(totalFriends) >= 0.6)

			// Check groups
			confirmedGroups := params.ConfirmedGroupsMap[originalInfo.ID]
			flaggedGroups := params.FlaggedGroupsMap[originalInfo.ID]
			totalInappropriateGroups := len(confirmedGroups) + len(flaggedGroups)
			totalGroups := len(originalInfo.Groups)
			hasSufficientInappropriateGroups := (totalInappropriateGroups >= 3 ||
				(totalGroups > 0 && float64(totalInappropriateGroups)/float64(totalGroups) >= 0.6)) &&
				totalInappropriateFriends >= 1

			// Skip if neither friends nor groups meet threshold
			if !hasSufficientInappropriateFriends && !hasSufficientInappropriateGroups {
				a.logger.Debug("Skipping low confidence user with insufficient inappropriate connections",
					zap.String("username", flaggedUser.Name),
					zap.Float64("confidence", flaggedUser.Confidence),
					zap.Int("inappropriateFriendsCount", totalInappropriateFriends),
					zap.Int("totalFriendsCount", totalFriends),
					zap.Int("inappropriateGroupsCount", totalInappropriateGroups),
					zap.Int("totalGroupsCount", totalGroups))

				return true
			}
		}
	}

	return false
}

// processAndCreateRequests processes the AI responses and creates reason requests.
func (a *UserAnalyzer) processAndCreateRequests(
	result *FlaggedUsers, params *ProcessUsersParams,
	userReasonRequests map[int64]UserReasonRequest, mu *sync.Mutex,
) {
	for _, flaggedUser := range result.Users {
		originalInfo, hasOriginal := params.OriginalInfos[flaggedUser.Name]
		if !hasOriginal {
			a.logger.Info("AI flagged non-existent user", zap.String("username", flaggedUser.Name))
			continue
		}

		// Get the translated info
		translatedInfo, hasTranslated := params.TranslatedInfos[flaggedUser.Name]
		if !hasTranslated {
			a.logger.Warn("Translated info not found for flagged user, using original",
				zap.String("username", flaggedUser.Name))

			translatedInfo = originalInfo
		}

		// Set the HasSocials field
		mu.Lock()

		originalInfo.HasSocials = flaggedUser.HasSocials

		mu.Unlock()

		// Check if this user should be skipped based on various conditions
		if a.shouldSkipFlaggedUser(flaggedUser, originalInfo, params) {
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

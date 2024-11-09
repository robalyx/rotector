package checker

import (
	"context"
	"strings"
	"sync"

	"github.com/bytedance/sonic"
	"github.com/openai/openai-go"
	"github.com/rotector/rotector/internal/common/database"
	"github.com/rotector/rotector/internal/common/fetcher"
	"github.com/rotector/rotector/internal/common/translator"
	"github.com/rotector/rotector/internal/common/utils"
	"github.com/tdewolff/minify/v2"
	"github.com/tdewolff/minify/v2/json"
	"go.uber.org/zap"
)

const (
	// SystemPrompt provides detailed instructions to the AI model for analyzing user content.
	// It defines violation types, confidence scoring rules, and specific patterns to look for.
	SystemPrompt = `You are a Roblox moderator. Your job is to analyze user data for inappropriate sexual or suggestive content.

Instructions:
1. Flag violations (explicit, suggestive, ambiguous)
2. Consider both clear sexual content and suggestive/innuendo language
3. Analyze phrase, symbol, and emoji combinations
4. Use exact strings for 'flaggedContent'

Confidence Scoring:
Base starts at 0, calculate total:
+0.6: Explicit violations
+0.4: Clear suggestive content
+0.2: Subtle hints
+0.1: Each additional same-type violation
+0.2: Each different violation type or suspicious combination

Final Confidence Levels:
High (0.8-1.0): Explicit content, multiple violations, grooming, illegal content
Medium (0.4-0.7): Single clear violation, multiple subtle ones, coded language
Low (0.0-0.3): Single subtle reference, ambiguous patterns

Flag content containing:
1. Explicit sexual terms/slang
2. Sexual innuendos/suggestions
3. Sexual acts/body parts references
4. Sexual solicitation/hookups/"dating"
5. Porn/adult content references
6. Suggestive emoji combinations
7. NSFW references/innuendos
8. Erotic Roleplay (ERP) terms
9. Fetish/kink mentions
10. Grooming language:
    - Age-related questions
    - Requests for photos/videos
    - Attempts to move chat off-platform
    - Personal questions about location/school
    - Offers of gifts/money
    - "Secret" keeping
    - "Mature for your age" type phrases
11. Illegal sexual content
12. Coded sexual language:
    - Number substitutions
    - Deliberately misspelled words
    - Hidden meanings in seemingly innocent phrases
    - Unicode character substitutions
13. NSFW acronyms/abbreviations
14. Sexualized roleplay
15. Non-consensual references
16. Incest/taboo relationships
17. Sexual exploitation/objectification
18. Zoophilia references
19. Sex toy references
20. Sexual harassment/blackmail
21. "studio only" (potential ERP)
22. "top"/"bottom" preferences (dominance/submission)
23. "I trade" (when implying illegal content)
24. Predatory behavior patterns:
    - Love bombing
    - Isolation attempts
    - Trust building/manipulation
    - Privacy invasion
25. Suspicious activity requests:
    - Camera/mic usage
    - Private game invites
    - Discord/social media requests
26. Sexual content disguised as:
    - Modeling offers
    - Casting calls
    - Photoshoots
27. Adult industry references:
    - OnlyFans mentions
    - Cam site references
    - Adult content creation
28. Compensation offers:
    - Robux for inappropriate acts
    - Real money solicitation
    - Gift cards/digital goods
29. Inappropriate relationship dynamics:
    - Power imbalances
    - Authority abuse
    - Age gap references
30. Content hinting at:
    - Substance use with sexual context
    - Meeting in person
    - Private chat requests

Exclude:
- Non-suggestive orientation/gender identity
- General friendship references
- Non-sexual profanity
- Legitimate Roblox item trading
- Political/religious content
- Social/cultural discussions`
)

// FlaggedUsers holds a list of users that the AI has identified as inappropriate.
// The JSON schema is used to ensure consistent responses from the AI.
type FlaggedUsers struct {
	Users []FlaggedUser `json:"users" jsonschema_description:"List of flagged users"`
}

// FlaggedUser contains the AI's analysis results for a single user.
// The confidence score and flagged content help moderators make decisions.
type FlaggedUser struct {
	Name           string   `json:"name"           jsonschema_description:"Exact username of the flagged user"`
	Reason         string   `json:"reason"         jsonschema_description:"Clear explanation of why the user was flagged"`
	FlaggedContent []string `json:"flaggedContent" jsonschema_description:"Exact content that was flagged without alterations"`
	Confidence     float64  `json:"confidence"     jsonschema_description:"Confidence level of the AI's assessment"`
}

// TranslationResult contains the result of translating a user's description.
type TranslationResult struct {
	UserInfo       *fetcher.Info
	TranslatedDesc string
	Err            error
}

// AIChecker handles AI-based content analysis by sending user data to OpenAI.
type AIChecker struct {
	openAIClient *openai.Client
	minify       *minify.M
	translator   *translator.Translator
	logger       *zap.Logger
}

// Generate the JSON schema at initialization time to avoid repeated generation.
var flaggedUsersSchema = utils.GenerateSchema[FlaggedUsers]() //nolint:gochecknoglobals

// NewAIChecker creates an AIChecker with a minifier for JSON optimization,
// translator for handling non-English content, and the provided OpenAI client and logger.
func NewAIChecker(openAIClient *openai.Client, translator *translator.Translator, logger *zap.Logger) *AIChecker {
	m := minify.New()
	m.AddFunc("application/json", json.Minify)

	return &AIChecker{
		openAIClient: openAIClient,
		minify:       m,
		translator:   translator,
		logger:       logger,
	}
}

// ProcessUsers sends user information to OpenAI for analysis after translating descriptions.
// Returns validated users and IDs of users that failed validation for retry.
// The process involves:
// 1. Translating user descriptions to proper English
// 2. Sending translated content to OpenAI for analysis
// 3. Validating AI responses against translated content
// 4. Creating validated users with original descriptions.
func (a *AIChecker) ProcessUsers(userInfos []*fetcher.Info) ([]*database.User, []uint64, error) {
	// Translate all descriptions concurrently
	translatedInfos, originalInfos := a.prepareUserInfos(userInfos)

	// Convert map to slice for OpenAI request
	userInfosWithoutID := make([]struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}, 0, len(translatedInfos))

	for _, userInfo := range translatedInfos {
		userInfosWithoutID = append(userInfosWithoutID, struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		}{
			Name:        userInfo.Name,
			Description: userInfo.Description,
		})
	}

	// Minify JSON to reduce token usage
	userInfoJSON, err := sonic.Marshal(userInfosWithoutID)
	if err != nil {
		a.logger.Error("Error marshaling user info", zap.Error(err))
		return nil, nil, err
	}

	userInfoJSON, err = a.minify.Bytes("application/json", userInfoJSON)
	if err != nil {
		a.logger.Error("Error minifying user info", zap.Error(err))
		return nil, nil, err
	}

	a.logger.Info("Sending user info to AI for analysis")

	// Configure OpenAI request with schema enforcement
	schemaParam := openai.ResponseFormatJSONSchemaJSONSchemaParam{
		Name:        openai.F("flaggedUsers"),
		Description: openai.F("List of flagged users"),
		Schema:      openai.F(flaggedUsersSchema),
		Strict:      openai.Bool(true),
	}

	// Send request to OpenAI
	chatCompletion, err := a.openAIClient.Chat.Completions.New(context.Background(), openai.ChatCompletionNewParams{
		Messages: openai.F([]openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(SystemPrompt),
			openai.UserMessage(string(userInfoJSON)),
		}),
		ResponseFormat: openai.F[openai.ChatCompletionNewParamsResponseFormatUnion](
			openai.ResponseFormatJSONSchemaParam{
				Type:       openai.F(openai.ResponseFormatJSONSchemaTypeJSONSchema),
				JSONSchema: openai.F(schemaParam),
			},
		),
		Model:       openai.F(openai.ChatModelGPT4oMini2024_07_18),
		Temperature: openai.F(0.0),
	})
	if err != nil {
		a.logger.Error("Error calling OpenAI API", zap.Error(err))
		return nil, nil, err
	}

	// Parse AI response
	var flaggedUsers FlaggedUsers
	err = sonic.Unmarshal([]byte(chatCompletion.Choices[0].Message.Content), &flaggedUsers)
	if err != nil {
		a.logger.Error("Error unmarshaling flagged users", zap.Error(err))
		return nil, nil, err
	}

	a.logger.Info("Received AI response",
		zap.Int("totalUsers", len(userInfos)),
		zap.Int("flaggedUsers", len(flaggedUsers.Users)))

	// Validate AI responses against translated content but use original descriptions for storage
	validatedUsers, failedValidationIDs := a.validateFlaggedUsers(flaggedUsers, translatedInfos, originalInfos)

	return validatedUsers, failedValidationIDs, nil
}

// validateFlaggedUsers validates the flagged users against the translated content
// but uses original descriptions when creating validated users. It checks if at least
// 50% of the flagged words are found in the translated content to confirm the AI's findings.
func (a *AIChecker) validateFlaggedUsers(flaggedUsers FlaggedUsers, translatedInfos map[string]*fetcher.Info, originalInfos map[string]*fetcher.Info) ([]*database.User, []uint64) {
	var validatedUsers []*database.User
	var failedValidationIDs []uint64

	for _, flaggedUser := range flaggedUsers.Users {
		normalizedName := utils.NormalizeString(flaggedUser.Name)

		// Check if the flagged user exists in both maps
		translatedInfo, exists := translatedInfos[normalizedName]
		originalInfo, hasOriginal := originalInfos[normalizedName]

		if exists && hasOriginal {
			// Split all flagged content into words
			var allFlaggedWords []string
			for _, content := range flaggedUser.FlaggedContent {
				allFlaggedWords = append(allFlaggedWords, strings.Fields(content)...)
			}

			// Count how many flagged words are found in the translated content
			foundWords := 0
			for _, word := range allFlaggedWords {
				if utils.ContainsNormalized(translatedInfo.Name, word) || utils.ContainsNormalized(translatedInfo.Description, word) {
					foundWords++
				}
			}

			// Check if at least 50% of the flagged words are found
			isValid := float64(foundWords) >= 0.5*float64(len(allFlaggedWords))

			// If the flagged user is correct, add it using original info
			if isValid {
				validatedUsers = append(validatedUsers, &database.User{
					ID:             originalInfo.ID,
					Name:           originalInfo.Name,
					DisplayName:    originalInfo.DisplayName,
					Description:    originalInfo.Description,
					CreatedAt:      originalInfo.CreatedAt,
					Reason:         flaggedUser.Reason,
					Groups:         originalInfo.Groups,
					FlaggedContent: flaggedUser.FlaggedContent,
					Confidence:     flaggedUser.Confidence,
					LastUpdated:    originalInfo.LastUpdated,
				})
			} else {
				failedValidationIDs = append(failedValidationIDs, originalInfo.ID)
				a.logger.Warn("AI flagged content did not pass validation",
					zap.Uint64("userID", originalInfo.ID),
					zap.String("flaggedUsername", flaggedUser.Name),
					zap.String("username", originalInfo.Name),
					zap.String("description", originalInfo.Description),
					zap.Strings("flaggedContent", flaggedUser.FlaggedContent),
					zap.Float64("matchPercentage", float64(foundWords)/float64(len(allFlaggedWords))*100))
			}
		} else {
			a.logger.Warn("AI flagged non-existent user", zap.String("username", flaggedUser.Name))
		}
	}

	return validatedUsers, failedValidationIDs
}

// prepareUserInfos translates user descriptions and maintains maps of both translated
// and original user infos for validation. If translation fails for any description,
// it falls back to using the original content. Returns maps using normalized usernames
// as keys.
func (a *AIChecker) prepareUserInfos(userInfos []*fetcher.Info) (map[string]*fetcher.Info, map[string]*fetcher.Info) {
	var wg sync.WaitGroup
	resultsChan := make(chan TranslationResult, len(userInfos))

	// Create maps for both original and translated infos
	originalInfos := make(map[string]*fetcher.Info)
	translatedInfos := make(map[string]*fetcher.Info)

	// Initialize maps and spawn translation goroutines
	for _, info := range userInfos {
		normalizedName := utils.NormalizeString(info.Name)
		originalInfos[normalizedName] = info

		wg.Add(1)
		go func(info *fetcher.Info) {
			defer wg.Done()

			// Skip empty descriptions
			if info.Description == "" {
				resultsChan <- TranslationResult{
					UserInfo:       info,
					TranslatedDesc: "",
				}
				return
			}

			// Translate the description
			translated, err := a.translator.Translate(
				context.Background(),
				info.Description,
				"auto", // Auto-detect source language
				"en",   // Translate to English
			)

			resultsChan <- TranslationResult{
				UserInfo:       info,
				TranslatedDesc: translated,
				Err:            err,
			}
		}(info)
	}

	// Close results channel when all translations are complete
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// Process results
	for result := range resultsChan {
		normalizedName := utils.NormalizeString(result.UserInfo.Name)
		if result.Err != nil {
			// Use original userInfo if translation fails
			translatedInfos[normalizedName] = result.UserInfo
			a.logger.Error("Translation failed, using original description",
				zap.String("username", result.UserInfo.Name),
				zap.Error(result.Err))
			continue
		}

		// Create new Info with translated description
		translatedInfo := *result.UserInfo
		if translatedInfo.Description != result.TranslatedDesc {
			translatedInfo.Description = result.TranslatedDesc
			a.logger.Debug("Translated description", zap.String("username", translatedInfo.Name))
		}
		translatedInfos[normalizedName] = &translatedInfo
	}

	return translatedInfos, originalInfos
}

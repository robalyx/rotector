package checker

import (
	"context"
	"fmt"

	"github.com/bytedance/sonic"
	"github.com/openai/openai-go"
	"github.com/rotector/rotector/internal/common/storage/database/models"
	"github.com/tdewolff/minify/v2"
	"github.com/tdewolff/minify/v2/json"
	"go.uber.org/zap"
)

const StatsSystemPrompt = `You are a witty assistant analyzing moderation statistics. Generate a short, engaging welcome message (max 512 characters) for moderators based on statistical trends. 

Instructions:
- Look for patterns and spikes in activity
- Stats show total counts, not differences
- Stats come from our automated detection system that flags suspicious users and groups
- Flagged items are those caught by our detection algorithms
- Confirmed items are those verified by moderators
- Mock their failed attempts to evade detection
- Celebrate their removal from the platform
- Make fun of their predictable behavior patterns
- Mock how easily our system catches them
- Mock their pathetic attempts to target minors
- Mock how they get caught trying to reach kids
- Ridicule their failed grooming tactics

Writing style:
- Keep sentences short and matter-of-fact
- Use dry sarcasm and deadpan humor about predators
- Each sentence should mock predators with a unique joke
- No greetings or dramatic words
- Keep jokes simple and direct, no complex metaphors
- Maximum 3 sentences

Example tones:
- "Another batch thinking they're smarter than kids. Our system proves they're not even smarter than algorithms. Thanks for the easy bans."
- "Imagine failing to outsmart both children and basic pattern matching. Detection rates show they're getting worse at hiding. Maybe try a new hobby."
- "Fresh group of predators thinking they're clever. Same old tricks getting flagged instantly. At least they're consistent at being obvious."
`

// StatsData represents the formatted statistics for AI analysis.
type StatsData struct {
	History []models.HourlyStats `json:"history" jsonschema_description:"Historical statistics from the last 24 hours"`
}

// StatsChecker analyzes statistics and generates welcome messages.
type StatsChecker struct {
	openAIClient *openai.Client
	minify       *minify.M
	logger       *zap.Logger
}

// NewStatsChecker creates a new stats checker instance.
func NewStatsChecker(openAIClient *openai.Client, logger *zap.Logger) *StatsChecker {
	m := minify.New()
	m.AddFunc("application/json", json.Minify)

	return &StatsChecker{
		openAIClient: openAIClient,
		minify:       m,
		logger:       logger,
	}
}

// GenerateWelcomeMessage analyzes current and historical stats to generate a contextual welcome message.
func (s *StatsChecker) GenerateWelcomeMessage(ctx context.Context, historicalStats []models.HourlyStats) (string, error) {
	// Format stats data for AI usage
	data := StatsData{
		History: historicalStats,
	}

	// Convert to JSON
	statsJSON, err := sonic.Marshal(data)
	if err != nil {
		s.logger.Error("Failed to marshal stats data", zap.Error(err))
		return "", err
	}

	// Minify JSON to reduce token usage
	statsJSON, err = s.minify.Bytes("application/json", statsJSON)
	if err != nil {
		s.logger.Error("Failed to minify stats data", zap.Error(err))
		return "", err
	}

	// Send request to OpenAI
	chatCompletion, err := s.openAIClient.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Messages: openai.F([]openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(StatsSystemPrompt),
			openai.UserMessage(string(statsJSON)),
		}),
		Model:       openai.F(openai.ChatModelGPT4oMini2024_07_18),
		Temperature: openai.F(1.0),
		MaxTokens:   openai.F(int64(512)),
	})
	if err != nil {
		return "", fmt.Errorf("OpenAI API error: %w", err)
	}

	message := chatCompletion.Choices[0].Message.Content

	s.logger.Debug("Generated welcome message",
		zap.String("message", message))

	return message, nil
}

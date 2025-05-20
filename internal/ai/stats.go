package ai

import (
	"context"
	"fmt"
	"time"

	"github.com/bytedance/sonic"
	"github.com/openai/openai-go"
	"github.com/robalyx/rotector/internal/ai/client"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/setup"
	"github.com/robalyx/rotector/pkg/utils"
	"github.com/tdewolff/minify/v2"
	"github.com/tdewolff/minify/v2/json"
	"go.uber.org/zap"
)

// StatsData represents the formatted statistics for AI analysis.
type StatsData struct {
	History []*types.HourlyStats `json:"history"`
}

// StatsAnalyzer analyzes statistics and generates welcome messages.
type StatsAnalyzer struct {
	chat   client.ChatCompletions
	minify *minify.M
	logger *zap.Logger
	model  string
}

// NewStatsAnalyzer creates a new stats analyzer instance.
func NewStatsAnalyzer(app *setup.App, logger *zap.Logger) *StatsAnalyzer {
	m := minify.New()
	m.AddFunc(ApplicationJSON, json.Minify)

	return &StatsAnalyzer{
		chat:   app.AIClient.Chat(),
		minify: m,
		logger: logger.Named("ai_stats"),
		model:  app.Config.Common.OpenAI.StatsModel,
	}
}

// GenerateWelcomeMessage analyzes current and historical stats to generate a contextual welcome message.
func (a *StatsAnalyzer) GenerateWelcomeMessage(
	ctx context.Context, historicalStats []*types.HourlyStats,
) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()

	// Format stats data for AI analysis
	data := StatsData{
		History: historicalStats,
	}

	// Convert stats to JSON
	statsJSON, err := sonic.Marshal(data)
	if err != nil {
		a.logger.Error("failed to marshal stats data", zap.Error(err))
		return "", fmt.Errorf("%w: %w", utils.ErrJSONProcessing, err)
	}

	// Minify JSON to reduce token usage
	statsJSON, err = a.minify.Bytes(ApplicationJSON, statsJSON)
	if err != nil {
		a.logger.Error("failed to minify stats data", zap.Error(err))
		return "", fmt.Errorf("%w: %w", utils.ErrJSONProcessing, err)
	}

	// Prepare chat completion parameters
	params := openai.ChatCompletionNewParams{
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(StatsSystemPrompt),
			openai.UserMessage(string(statsJSON)),
		},
		Model:               a.model,
		Temperature:         openai.Float(0.7),
		TopP:                openai.Float(0.7),
		MaxCompletionTokens: openai.Int(512),
	}

	// Generate welcome message
	var response string
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
			a.logger.Debug("AI stats analysis thought process",
				zap.String("model", resp.Model),
				zap.String("thought", thought.Raw()))
		}

		response = utils.CompressAllWhitespace(message.Content)
		return nil
	})
	if err != nil {
		return "", err
	}

	a.logger.Debug("Generated welcome message",
		zap.String("message", response))

	return response, nil
}

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

// StatsSystemPrompt is the system prompt for the stats analyzer.
const StatsSystemPrompt = `Instruction:
You are a witty assistant analyzing moderation statistics.
Generate a short, engaging welcome message (max 512 characters) for moderators based on statistical trends. 

Key instructions:
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
- Create exactly one sentence that combines multiple observations
- Use conjunctions to connect ideas smoothly
- Use dry sarcasm and deadpan humor about predators
- Keep the tone matter-of-fact while being witty
- No greetings or dramatic words
- Keep jokes simple and direct, no complex metaphors

Example responses (format reference only):
- "Our algorithms caught another wave of predators trying their tired old tricks 
  while attempts to hide got more pathetic by the hour 
  and their desperate messages never made it past our filters."
- "The detection system effortlessly caught today's batch of predators 
  as they fumbled through increasingly obvious tactics 
  and failed to realize how transparent their pathetic attempts had become."
- "While predators kept trying to outsmart our system with their predictable patterns 
  and desperate tactics, our algorithms were already three steps ahead 
  and swiftly removed their accounts."`

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
		model:  app.Config.Common.OpenAI.Model,
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
		return "", fmt.Errorf("%w: %w", ErrJSONProcessing, err)
	}

	// Minify JSON to reduce token usage
	statsJSON, err = a.minify.Bytes(ApplicationJSON, statsJSON)
	if err != nil {
		a.logger.Error("failed to minify stats data", zap.Error(err))
		return "", fmt.Errorf("%w: %w", ErrJSONProcessing, err)
	}

	// Generate welcome message with retry
	response, err := utils.WithRetry(ctx, func() (string, error) {
		resp, err := a.chat.New(ctx, openai.ChatCompletionNewParams{
			Messages: []openai.ChatCompletionMessageParamUnion{
				openai.SystemMessage(StatsSystemPrompt),
				openai.UserMessage(string(statsJSON)),
			},
			Model:               a.model,
			Temperature:         openai.Float(0.7),
			TopP:                openai.Float(0.7),
			MaxCompletionTokens: openai.Int(512),
		})
		if err != nil {
			return "", fmt.Errorf("openai API error: %w", err)
		}

		// Check for empty response
		if len(resp.Choices) == 0 || len(resp.Choices[0].Message.Content) == 0 {
			return "", fmt.Errorf("%w: no response from model", ErrModelResponse)
		}

		// Extract response text
		content := resp.Choices[0].Message.Content
		return utils.CompressAllWhitespace(content), nil
	}, utils.GetAIRetryOptions())
	if err != nil {
		return "", err
	}

	a.logger.Debug("Generated welcome message",
		zap.String("message", response))

	return response, nil
}

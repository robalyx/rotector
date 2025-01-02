package ai

import (
	"context"
	"fmt"

	"github.com/bytedance/sonic"
	"github.com/google/generative-ai-go/genai"
	"github.com/rotector/rotector/internal/common/setup"
	"github.com/rotector/rotector/internal/common/storage/database/types"
	"github.com/rotector/rotector/internal/common/utils"
	"github.com/tdewolff/minify/v2"
	"github.com/tdewolff/minify/v2/json"
	"go.uber.org/zap"
)

// StatsSystemPrompt is the system prompt for the stats analyzer.
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
- Create exactly one sentence that combines multiple observations
- Use conjunctions to connect ideas smoothly
- Use dry sarcasm and deadpan humor about predators
- Keep the tone matter-of-fact while being witty
- No greetings or dramatic words
- Keep jokes simple and direct, no complex metaphors

Example responses (format reference only):
- "Our algorithms caught another wave of predators trying their tired old tricks while attempts to hide got more pathetic by the hour and their desperate messages never made it past our filters."
- "The detection system effortlessly caught today's batch of predators as they fumbled through increasingly obvious tactics and failed to realize how transparent their pathetic attempts had become."
- "While predators kept trying to outsmart our system with their predictable patterns and desperate tactics, our algorithms were already three steps ahead and swiftly removed their accounts."`

// MaxOutputTokens is the maximum number of tokens in the response.
const MaxOutputTokens = 512

// StatsData represents the formatted statistics for AI analysis.
type StatsData struct {
	History []*types.HourlyStats `json:"history"`
}

// StatsAnalyzer analyzes statistics and generates welcome messages.
type StatsAnalyzer struct {
	genModel *genai.GenerativeModel
	minify   *minify.M
	logger   *zap.Logger
}

// NewStatsAnalyzer creates a new stats analyzer instance.
func NewStatsAnalyzer(app *setup.App, logger *zap.Logger) *StatsAnalyzer {
	// Create a new Gemini model
	model := app.GenAIClient.GenerativeModel(app.Config.Common.GeminiAI.Model)
	model.SystemInstruction = genai.NewUserContent(genai.Text(StatsSystemPrompt))

	// Configure model to return plain text
	maxTokens := int32(MaxOutputTokens)
	temperature := float32(0.7)
	model.GenerationConfig.ResponseMIMEType = "text/plain"
	model.GenerationConfig.MaxOutputTokens = &maxTokens
	model.GenerationConfig.Temperature = &temperature

	// Create a minifier for JSON optimization
	m := minify.New()
	m.AddFunc("application/json", json.Minify)

	return &StatsAnalyzer{
		genModel: model,
		minify:   m,
		logger:   logger,
	}
}

// GenerateWelcomeMessage analyzes current and historical stats to generate a contextual welcome message.
func (a *StatsAnalyzer) GenerateWelcomeMessage(ctx context.Context, historicalStats []*types.HourlyStats) (string, error) {
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
	statsJSON, err = a.minify.Bytes("application/json", statsJSON)
	if err != nil {
		a.logger.Error("failed to minify stats data", zap.Error(err))
		return "", fmt.Errorf("%w: %w", ErrJSONProcessing, err)
	}

	// Generate welcome message using Gemini model with retry
	message, err := withRetry(ctx, func() (string, error) {
		resp, err := a.genModel.GenerateContent(ctx, genai.Text(string(statsJSON)))
		if err != nil {
			return "", fmt.Errorf("gemini API error: %w", err)
		}

		// Check for empty response
		if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
			return "", fmt.Errorf("%w: no response from model", ErrModelResponse)
		}

		// Extract response text
		text := string(resp.Candidates[0].Content.Parts[0].(genai.Text))
		cleanMessage := utils.CleanupText(text)

		return cleanMessage, nil
	})
	if err != nil {
		return "", err
	}

	a.logger.Debug("Generated welcome message",
		zap.String("message", message))

	return message, nil
}

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
- Keep sentences short and matter-of-fact
- Use dry sarcasm and deadpan humor about predators
- Each sentence should mock predators with a unique joke
- No greetings or dramatic words
- Keep jokes simple and direct, no complex metaphors
- Maximum 4 sentences

Example responses (format reference only):
- "Predators keep trying new tricks but our algorithms catch them faster than they can type. Their attempts to hide get more pathetic by the day. Another successful batch of bans."
- "Today's predators thought they were clever until our system proved otherwise. Their attempts to evade detection are getting lazier. At least they make our job entertaining."
- "Our detection rates show predators are getting worse at hiding. Their failed attempts at grooming are becoming more obvious. The system catches them faster than they can make new accounts."`

// MaxOutputTokens is the maximum number of tokens in the response.
const MaxOutputTokens = 512

// StatsData represents the formatted statistics for AI analysis.
type StatsData struct {
	History []types.HourlyStats `json:"history"`
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
func (a *StatsAnalyzer) GenerateWelcomeMessage(ctx context.Context, historicalStats []types.HourlyStats) (string, error) {
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

	// Generate welcome message using Gemini model
	resp, err := a.genModel.GenerateContent(ctx, genai.Text(string(statsJSON)))
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrModelResponse, err)
	}

	// Check for empty response
	if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("%w: no response from model", ErrModelResponse)
	}

	// Extract response text from Gemini's response
	message := resp.Candidates[0].Content.Parts[0].(genai.Text)

	// Clean up the response text
	cleanMessage := utils.CleanupText(string(message))

	a.logger.Debug("Generated welcome message",
		zap.String("message", cleanMessage))

	return cleanMessage, nil
}

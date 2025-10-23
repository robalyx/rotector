package manager

import (
	"context"
	"fmt"
	"time"

	"github.com/robalyx/rotector/internal/cloudflare/api"
	"go.uber.org/zap"
)

// AIUsage handles AI usage tracking operations.
type AIUsage struct {
	d1     *api.D1Client
	logger *zap.Logger
}

// NewAIUsage creates a new AI usage manager.
func NewAIUsage(d1Client *api.D1Client, logger *zap.Logger) *AIUsage {
	return &AIUsage{
		d1:     d1Client,
		logger: logger,
	}
}

// UpdateDailyUsage updates the daily AI usage statistics in the D1 database.
func (a *AIUsage) UpdateDailyUsage(
	ctx context.Context, date string, promptTokens int64,
	completionTokens int64, reasoningTokens int64, cost float64,
) error {
	upsertQuery := `
		INSERT INTO ai_daily_usage (
			date,
			usage,
			requests,
			prompt_tokens,
			completion_tokens,
			reasoning_tokens,
			last_synced
		)
		VALUES (?, ?, 1, ?, ?, ?, ?)
		ON CONFLICT(date) DO UPDATE SET
			usage = usage + excluded.usage,
			requests = requests + 1,
			prompt_tokens = prompt_tokens + excluded.prompt_tokens,
			completion_tokens = completion_tokens + excluded.completion_tokens,
			reasoning_tokens = reasoning_tokens + excluded.reasoning_tokens,
			last_synced = excluded.last_synced
	`

	currentTime := time.Now().Unix()
	params := []any{
		date,
		cost,
		promptTokens,
		completionTokens,
		reasoningTokens,
		currentTime,
	}

	_, err := a.d1.ExecuteSQL(ctx, upsertQuery, params)
	if err != nil {
		return fmt.Errorf("failed to update daily usage: %w", err)
	}

	a.logger.Debug("Updated AI daily usage",
		zap.String("date", date),
		zap.Int64("prompt_tokens", promptTokens),
		zap.Int64("completion_tokens", completionTokens),
		zap.Int64("reasoning_tokens", reasoningTokens),
		zap.Float64("cost", cost))

	return nil
}

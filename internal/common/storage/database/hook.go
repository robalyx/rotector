package database

import (
	"context"
	"time"

	"github.com/uptrace/bun"
	"go.uber.org/zap"
)

// Hook implements bun.QueryHook interface for logging queries with zap.
type Hook struct {
	logger *zap.Logger
}

// NewHook creates a new Hook with zap logger.
func NewHook(logger *zap.Logger) *Hook {
	return &Hook{logger: logger}
}

// BeforeQuery logs the query before execution.
func (h *Hook) BeforeQuery(ctx context.Context, _ *bun.QueryEvent) context.Context {
	return ctx
}

// AfterQuery logs the query and its execution time.
func (h *Hook) AfterQuery(_ context.Context, event *bun.QueryEvent) {
	duration := time.Since(event.StartTime)

	// Track transaction boundaries
	if event.Query == "BEGIN" || event.Query == "COMMIT" || event.Query == "ROLLBACK" {
		if duration > 200*time.Millisecond {
			h.logger.Warn("Slow transaction boundary",
				zap.String("operation", event.Query),
				zap.Duration("duration", duration))
		}
		return
	}

	// Track long queries
	if duration > time.Second {
		h.logger.Warn("Slow query detected",
			zap.String("query", event.Query),
			zap.Duration("duration", duration))
	}
}

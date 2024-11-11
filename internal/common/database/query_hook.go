package database

import (
	"context"
	"time"

	"github.com/uptrace/bun"
	"go.uber.org/zap"
)

// QueryHook implements bun.QueryHook interface for logging queries with zap.
type QueryHook struct {
	logger *zap.Logger
}

// NewQueryHook creates a new QueryHook with zap logger.
func NewQueryHook(logger *zap.Logger) *QueryHook {
	return &QueryHook{logger: logger}
}

// BeforeQuery logs the query before execution.
func (h *QueryHook) BeforeQuery(ctx context.Context, _ *bun.QueryEvent) context.Context {
	return ctx
}

// AfterQuery logs the query and its execution time.
func (h *QueryHook) AfterQuery(_ context.Context, event *bun.QueryEvent) {
	// Log query with different levels based on error
	if event.Err != nil {
		h.logger.Error("Query failed",
			zap.String("query", event.Query),
			zap.Duration("duration", time.Since(event.StartTime)),
			zap.Error(event.Err))
	} else {
		h.logger.Debug("Query executed",
			zap.String("query", event.Query),
			zap.Duration("duration", time.Since(event.StartTime)))
	}
}

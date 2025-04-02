package telemetry

import (
	"context"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap/zapcore"
)

// Core implements zapcore.Core to forward logs to OpenTelemetry.
type Core struct {
	zapcore.LevelEnabler
	tracer trace.Tracer
}

// NewCore creates a new core that forwards logs to OpenTelemetry.
func NewCore(enab zapcore.LevelEnabler) zapcore.Core {
	return &Core{
		LevelEnabler: enab,
		tracer:       otel.Tracer("logs"),
	}
}

func (c *Core) With(_ []zapcore.Field) zapcore.Core {
	return c
}

func (c *Core) Check(ent zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(ent.Level) {
		return ce.AddCore(ent, c)
	}
	return ce
}

func (c *Core) Write(ent zapcore.Entry, fields []zapcore.Field) error {
	// Only forward Error and higher severity
	if ent.Level < zapcore.ErrorLevel {
		return nil
	}

	spanName := "error." + getErrorCategory(ent)
	_, span := c.tracer.Start(context.Background(), spanName)
	defer span.End()

	// Add log entry details as span attributes
	attrs := []attribute.KeyValue{
		attribute.String("error.message", ent.Message),
		attribute.String("error.level", ent.Level.String()),
		attribute.String("error.caller", ent.Caller.String()),
	}

	// Add any additional fields
	for _, field := range fields {
		attrs = append(attrs, attribute.String(field.Key, field.String))
	}

	span.SetAttributes(attrs...)
	return nil
}

func (c *Core) Sync() error {
	return nil
}

// getErrorCategory determines the error category based on the log entry.
func getErrorCategory(ent zapcore.Entry) string {
	// Common categories based on the caller package or error patterns
	switch {
	case strings.Contains(ent.Caller.Function, "database"):
		return "database"
	case strings.Contains(ent.Caller.Function, "redis"):
		return "redis"
	case strings.Contains(ent.Caller.Function, "bot"):
		return "bot"
	case strings.Contains(ent.Caller.Function, "worker"):
		return "worker"
	case strings.Contains(ent.Caller.Function, "setup"):
		return "setup"
	default:
		return "application"
	}
}

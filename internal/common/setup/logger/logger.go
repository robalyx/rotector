package logger

import (
	"github.com/jaxron/axonet/pkg/client/logger"
	"go.uber.org/zap"
)

// Logger adapts zap.Logger to implement the axonet logger.Logger interface.
type Logger struct {
	zap *zap.Logger
}

// New creates a new Logger instance that wraps a zap.Logger.
// This adapter allows zap.Logger to be used with the axonet logging interface.
func New(zapLogger *zap.Logger) logger.Logger {
	return &Logger{zap: zapLogger}
}

func (l *Logger) Debug(msg string)                  { l.zap.Debug(msg) }
func (l *Logger) Info(msg string)                   { l.zap.Info(msg) }
func (l *Logger) Warn(msg string)                   { l.zap.Warn(msg) }
func (l *Logger) Error(msg string)                  { l.zap.Error(msg) }
func (l *Logger) Debugf(format string, args ...any) { l.zap.Sugar().Debugf(format, args...) }
func (l *Logger) Infof(format string, args ...any)  { l.zap.Sugar().Infof(format, args...) }
func (l *Logger) Warnf(format string, args ...any)  { l.zap.Sugar().Warnf(format, args...) }
func (l *Logger) Errorf(format string, args ...any) { l.zap.Sugar().Errorf(format, args...) }

// WithFields creates a new logger with additional context fields.
// It converts axonet fields to zap fields and creates a new logger instance.
func (l *Logger) WithFields(fields ...logger.Field) logger.Logger {
	zapFields := make([]zap.Field, len(fields))
	for i, f := range fields {
		zapFields[i] = zap.Any(f.Key, f.Value)
	}
	return &Logger{zap: l.zap.With(zapFields...)}
}

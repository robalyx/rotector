package telemetry

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/getsentry/sentry-go"
	"go.uber.org/zap/zapcore"
)

// SentryCore implements zapcore.Core interface to forward errors to Sentry.
type SentryCore struct {
	zapcore.LevelEnabler
}

// NewSentryCore creates a new Core that forwards errors to Sentry.
func NewSentryCore(enab zapcore.LevelEnabler) *SentryCore {
	return &SentryCore{LevelEnabler: enab}
}

// With adds structured context to the Core.
func (c *SentryCore) With(_ []zapcore.Field) zapcore.Core {
	return c
}

// Check determines whether the supplied Entry should be logged.
func (c *SentryCore) Check(ent zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(ent.Level) {
		return ce.AddCore(ent, c)
	}

	return ce
}

// Write forwards error and fatal level logs to Sentry.
func (c *SentryCore) Write(ent zapcore.Entry, fields []zapcore.Field) error {
	// Only forward error and fatal level entries to Sentry
	if ent.Level >= zapcore.ErrorLevel && sentry.CurrentHub().Client() != nil {
		sentry.WithScope(func(scope *sentry.Scope) {
			// Convert zap fields to Sentry extras
			enc := zapcore.NewMapObjectEncoder()

			var errorValues []string

			for i := range fields {
				// Check if this is an error field
				if fields[i].Type == zapcore.ErrorType {
					// Get the error value directly
					if err, ok := fields[i].Interface.(error); ok {
						errorValues = append(errorValues, err.Error())
					}
				}

				fields[i].AddTo(enc)
			}

			// Add fields as extras in Sentry
			for k, v := range enc.Fields {
				if k != "error" { // Skip error field as we handle it separately
					scope.SetExtra(k, v)
				}
			}

			// Set error level based on zap level
			var level sentry.Level
			switch ent.Level {
			case zapcore.ErrorLevel:
				level = sentry.LevelError
			case zapcore.DPanicLevel, zapcore.PanicLevel:
				level = sentry.LevelFatal
			case zapcore.FatalLevel:
				level = sentry.LevelFatal
			default:
				level = sentry.LevelInfo
			}

			scope.SetLevel(level)

			// Extract package path and function name from caller
			var packagePath, funcName string

			if ent.Caller.Function != "" {
				lastSlash := strings.LastIndexByte(ent.Caller.Function, '/')
				if lastSlash > -1 {
					packagePath = ent.Caller.Function[:lastSlash]
				}

				lastDot := strings.LastIndexByte(ent.Caller.Function, '.')
				if lastDot > -1 {
					funcName = ent.Caller.Function[lastDot+1:]
				} else {
					funcName = ent.Caller.Function
				}
			}

			// Create an exception event
			event := sentry.NewEvent()
			event.Level = level
			event.Message = ent.Message

			// If we have error values, include them in the exception
			exceptionValue := ent.Message

			if len(errorValues) > 0 {
				errStr := strings.Join(errorValues, "; ")
				exceptionValue = fmt.Sprintf("%s: %s", ent.Message, errStr)
			}

			event.Exception = []sentry.Exception{{
				Value:      exceptionValue,
				Type:       funcName,
				Module:     packagePath,
				Stacktrace: sentry.NewStacktrace(),
			}}

			// Add source code location context
			event.Exception[0].Module = filepath.Dir(ent.Caller.File)
			event.Exception[0].Type = funcName

			sentry.CaptureEvent(event)
		})
	}

	return nil
}

// Sync implements zapcore.Core.
func (c *SentryCore) Sync() error {
	return nil
}

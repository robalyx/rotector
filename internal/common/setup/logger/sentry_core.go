package logger

import (
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
			for i := range fields {
				fields[i].AddTo(enc)
			}

			// Add fields as extras in Sentry
			for k, v := range enc.Fields {
				scope.SetExtra(k, v)
			}

			// Set stack trace
			if ent.Stack != "" {
				scope.SetExtra("stacktrace", ent.Stack)
			}

			// Capture the error
			sentry.CaptureMessage(ent.Message)
		})
	}

	return nil
}

// Sync implements zapcore.Core.
func (c *SentryCore) Sync() error {
	return nil
}

package loki

import (
	"encoding/json"

	"go.uber.org/zap/zapcore"
)

// Core implements zapcore.Core interface for Loki log shipping.
type Core struct {
	zapcore.LevelEnabler

	pusher *Pusher
}

// NewCore creates a new Loki Core with the provided pusher.
func NewCore(enabler zapcore.LevelEnabler, pusher *Pusher) *Core {
	return &Core{
		LevelEnabler: enabler,
		pusher:       pusher,
	}
}

// With returns a new Core instance preserving the current configuration.
func (c *Core) With(_ []zapcore.Field) zapcore.Core {
	return &Core{
		LevelEnabler: c.LevelEnabler,
		pusher:       c.pusher,
	}
}

// Check determines whether the supplied Entry should be logged.
func (c *Core) Check(ent zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(ent.Level) {
		return ce.AddCore(ent, c)
	}

	return ce
}

// Write converts the log entry to JSON and sends it to Loki.
func (c *Core) Write(ent zapcore.Entry, fields []zapcore.Field) error {
	// Convert entry to our logEntry format
	entry := logEntry{
		Level:     ent.Level.String(),
		Timestamp: float64(ent.Time.UnixMilli()),
		Message:   ent.Message,
		Caller:    ent.Caller.TrimmedPath(),
		Fields:    make(map[string]interface{}),
	}

	// Add stack trace for errors
	if ent.Stack != "" {
		entry.Stack = ent.Stack
	}

	// Convert zap fields to map
	enc := zapcore.NewMapObjectEncoder()
	for i := range fields {
		fields[i].AddTo(enc)
	}

	// Merge fields into entry
	for k, v := range enc.Fields {
		entry.Fields[k] = v
	}

	// Marshal to JSON to get the raw string
	raw, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	entry.raw = string(raw)

	// Send to Loki pusher
	c.pusher.AddEntry(entry)

	return nil
}

// Sync flushes any buffered log entries.
func (c *Core) Sync() error {
	// The pusher handles batching and sending asynchronously,
	// so we don't need to do anything special here
	return nil
}

package session

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"

	"github.com/redis/rueidis"

	"github.com/bytedance/sonic"
	"github.com/rotector/rotector/internal/common/database"

	"go.uber.org/zap"
)

// Session represents a user's session.
type Session struct {
	db     *database.Database
	redis  rueidis.Client
	key    string
	data   map[string]string
	logger *zap.Logger
}

// NewSession creates a new session for the given user.
func NewSession(db *database.Database, redis rueidis.Client, key string, data map[string]string, logger *zap.Logger) *Session {
	return &Session{
		db:     db,
		redis:  redis,
		key:    key,
		data:   data,
		logger: logger,
	}
}

// Touch updates the session's expiration time.
func (s *Session) Touch(ctx context.Context) {
	// Serialize session data
	data, err := sonic.MarshalString(s.data)
	if err != nil {
		s.logger.Error("Failed to marshal session data", zap.Error(err))
		return
	}

	// Set data with expiration in Redis
	err = s.redis.Do(ctx,
		s.redis.B().Set().Key(s.key).Value(data).Ex(SessionTimeout).Build(),
	).Error()
	if err != nil {
		s.logger.Error("Failed to update session in Redis", zap.Error(err))
	}
}

// Get returns the raw string value for the given key.
func (s *Session) Get(key string) string {
	if value, ok := s.data[key]; ok {
		return value
	}
	s.logger.Warn("Session key not found", zap.String("key", key))
	return ""
}

// Set sets the value for the given key.
func (s *Session) Set(key string, value interface{}) {
	// Convert value to JSON string
	jsonStr, err := sonic.MarshalString(value)
	if err != nil {
		s.logger.Error("Failed to marshal value", zap.Error(err), zap.String("key", key))
		return
	}
	s.data[key] = jsonStr
	s.logger.Debug("Session key set", zap.String("key", key))
}

// SetBuffer stores a buffer in the session after base64 encoding it.
func (s *Session) SetBuffer(key string, buf *bytes.Buffer) {
	if buf == nil {
		s.logger.Warn("Attempted to set nil buffer", zap.String("key", key))
		return
	}

	// Encode buffer to base64
	encoded := base64.StdEncoding.EncodeToString(buf.Bytes())
	s.data[key] = encoded
	s.logger.Debug("Session key set with base64 encoded buffer", zap.String("key", key))
}

// Delete deletes the value for the given key.
func (s *Session) Delete(key string) {
	delete(s.data, key)
	s.logger.Debug("Session key deleted", zap.String("key", key))
}

// GetString returns the string value for the given key.
func (s *Session) GetString(key string) string {
	var value string
	s.GetInterface(key, &value)
	return value
}

// GetInt returns the integer value for the given key.
func (s *Session) GetInt(key string) int {
	var value int
	s.GetInterface(key, &value)
	return value
}

// GetUint64 returns the uint64 value for the given key.
func (s *Session) GetUint64(key string) uint64 {
	var value uint64
	s.GetInterface(key, &value)
	return value
}

// GetFloat64 returns the float64 value for the given key.
func (s *Session) GetFloat64(key string) float64 {
	var value float64
	s.GetInterface(key, &value)
	return value
}

// GetBool returns the boolean value for the given key.
func (s *Session) GetBool(key string) bool {
	var value bool
	s.GetInterface(key, &value)
	return value
}

// GetInterface unmarshals the stored JSON string into the provided interface.
func (s *Session) GetInterface(key string, v interface{}) {
	jsonStr := s.Get(key)
	if jsonStr == "" {
		s.logger.Warn("Session key empty", zap.String("key", key))
		return
	}

	if err := sonic.UnmarshalString(jsonStr, v); err != nil {
		s.logger.Error("Failed to unmarshal interface",
			zap.Error(err),
			zap.String("key", key),
			zap.String("json", jsonStr),
			zap.String("type", fmt.Sprintf("%T", v)))
		return
	}
	s.logger.Debug("Session key value", zap.String("key", key), zap.Any("value", v))
}

// GetBuffer retrieves and decodes a base64 encoded buffer from the session.
func (s *Session) GetBuffer(key string) *bytes.Buffer {
	str := s.Get(key)
	if str == "" {
		return nil
	}

	// Try to decode base64
	decoded, err := base64.StdEncoding.DecodeString(str)
	if err != nil {
		s.logger.Error("Failed to decode base64 buffer",
			zap.Error(err),
			zap.String("key", key))
		return nil
	}

	return bytes.NewBuffer(decoded)
}

package session

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"

	"github.com/redis/rueidis"
	"github.com/spf13/cast"

	"github.com/bytedance/sonic"
	"github.com/rotector/rotector/internal/common/storage/database"
	"go.uber.org/zap"
)

// Session maintains user state through a Redis-backed key-value store where values are
// serialized as JSON strings. The session automatically expires after a configured timeout.
type Session struct {
	db     *database.Client
	redis  rueidis.Client
	key    string
	data   map[string]interface{}
	logger *zap.Logger
}

// NewSession creates a new session for the given user.
func NewSession(db *database.Client, redis rueidis.Client, key string, data map[string]interface{}, logger *zap.Logger) *Session {
	return &Session{
		db:     db,
		redis:  redis,
		key:    key,
		data:   data,
		logger: logger,
	}
}

// Touch serializes the session data to JSON and updates the TTL in Redis to prevent expiration.
// If serialization fails, the error is logged but the session continues.
func (s *Session) Touch(ctx context.Context) {
	// Serialize session data to JSON
	data, err := sonic.MarshalString(s.data)
	if err != nil {
		s.logger.Error("Failed to marshal session data", zap.Error(err))
		return
	}

	// Update Redis with new data and expiration
	err = s.redis.Do(ctx,
		s.redis.B().Set().Key(s.key).Value(data).Ex(SessionTimeout).Build(),
	).Error()
	if err != nil {
		s.logger.Error("Failed to update session in Redis", zap.Error(err))
	}
}

// Get retrieves a raw string value from the in-memory session cache.
// Returns empty string if key doesn't exist.
func (s *Session) Get(key string) interface{} {
	if value, ok := s.data[key]; ok {
		return value
	}
	s.logger.Debug("Session key not found", zap.String("key", key))
	return nil
}

// GetInterface unmarshals the stored value into the provided interface.
func (s *Session) GetInterface(key string, v interface{}) {
	value := s.Get(key)
	if value == nil {
		return
	}

	// Marshal the value back to JSON
	jsonBytes, err := sonic.Marshal(value)
	if err != nil {
		s.logger.Error("Failed to marshal interface",
			zap.Error(err),
			zap.String("key", key),
			zap.Any("value", value))
		return
	}

	// Unmarshal into the target interface
	if err := sonic.Unmarshal(jsonBytes, v); err != nil {
		s.logger.Error("Failed to unmarshal interface",
			zap.Error(err),
			zap.String("key", key),
			zap.String("json", string(jsonBytes)),
			zap.String("type", fmt.Sprintf("%T", v)))
		return
	}
}

// GetBuffer retrieves and decodes a base64 encoded buffer from the session.
func (s *Session) GetBuffer(key string) *bytes.Buffer {
	value := s.Get(key)
	if value == nil {
		return nil
	}

	str, ok := value.(string)
	if !ok {
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

// Set sets the value for the given key.
func (s *Session) Set(key string, value interface{}) {
	s.data[key] = value
	s.logger.Debug("Session key set", zap.String("key", key))
}

// SetBuffer stores binary data by base64 encoding it first.
// This allows binary data to be safely stored as strings in the session.
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

// Delete removes a key from the session data.
func (s *Session) Delete(key string) {
	delete(s.data, key)
	s.logger.Debug("Session key deleted", zap.String("key", key))
}

// GetString retrieves a string value from the session.
func (s *Session) GetString(key string) string {
	if value := s.Get(key); value != nil {
		if str, ok := value.(string); ok {
			return str
		}
	}
	return ""
}

// GetInt retrieves an integer value from the session.
func (s *Session) GetInt(key string) int {
	if value := s.Get(key); value != nil {
		return cast.ToInt(value)
	}
	return 0
}

// GetUint64 retrieves an unsigned 64-bit integer value from the session.
func (s *Session) GetUint64(key string) uint64 {
	if value := s.Get(key); value != nil {
		return cast.ToUint64(value)
	}
	return 0
}

// GetFloat64 retrieves a float64 value from the session.
func (s *Session) GetFloat64(key string) float64 {
	if value := s.Get(key); value != nil {
		return cast.ToFloat64(value)
	}
	return 0
}

// GetBool retrieves a boolean value from the session.
func (s *Session) GetBool(key string) bool {
	if value := s.Get(key); value != nil {
		return cast.ToBool(value)
	}
	return false
}

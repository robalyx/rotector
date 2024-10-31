package session

import (
	"context"

	"github.com/bytedance/sonic"
	"github.com/redis/rueidis"
	"github.com/rotector/rotector/internal/common/database"
	"go.uber.org/zap"
)

// Session represents a user's session.
type Session struct {
	db     *database.Database
	redis  rueidis.Client
	key    string
	data   map[string]interface{}
	logger *zap.Logger
}

// NewSession creates a new session for the given user.
func NewSession(db *database.Database, redis rueidis.Client, key string, logger *zap.Logger) *Session {
	return &Session{
		db:     db,
		redis:  redis,
		key:    key,
		data:   make(map[string]interface{}),
		logger: logger,
	}
}

// Touch updates the session's expiration time.
func (s *Session) Touch(ctx context.Context) {
	// Serialize session data
	data, err := sonic.Marshal(s.data)
	if err != nil {
		s.logger.Error("Failed to marshal session data", zap.Error(err))
		return
	}

	// Set data with expiration in Redis
	err = s.redis.Do(ctx,
		s.redis.B().Set().Key(s.key).Value(string(data)).Ex(SessionTimeout).Build(),
	).Error()
	if err != nil {
		s.logger.Error("Failed to update session in Redis", zap.Error(err))
	}
}

// Get returns the value for the given key.
func (s *Session) Get(key string) interface{} {
	if value, ok := s.data[key]; ok {
		return value
	}
	s.logger.Warn("Session key not found", zap.String("key", key))
	return nil
}

// Set sets the value for the given key.
func (s *Session) Set(key string, value interface{}) {
	s.data[key] = value
	s.logger.Debug("Session key set", zap.String("key", key))
}

// Delete deletes the value for the given key.
func (s *Session) Delete(key string) {
	delete(s.data, key)
	s.logger.Debug("Session key deleted", zap.String("key", key))
}

// GetString returns the string value for the given key.
func (s *Session) GetString(key string) string {
	if value, ok := s.Get(key).(string); ok {
		return value
	}
	return ""
}

// GetInt returns the integer value for the given key.
func (s *Session) GetInt(key string) int {
	if value, ok := s.Get(key).(int); ok {
		return value
	}
	return 0
}

// GetUint64 returns the uint64 value for the given key.
func (s *Session) GetUint64(key string) uint64 {
	if value, ok := s.Get(key).(uint64); ok {
		return value
	}
	return 0
}

// GetFloat64 returns the float64 value for the given key.
func (s *Session) GetFloat64(key string) float64 {
	if value, ok := s.Get(key).(float64); ok {
		return value
	}
	return 0
}

// GetBool returns the boolean value for the given key.
func (s *Session) GetBool(key string) bool {
	if value, ok := s.Get(key).(bool); ok {
		return value
	}
	return false
}

// GetFlaggedUser returns the flagged user for the given key.
func (s *Session) GetFlaggedUser(key string) *database.FlaggedUser {
	if value, ok := s.Get(key).(*database.FlaggedUser); ok {
		return value
	}
	return nil
}

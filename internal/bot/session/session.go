package session

import (
	"sync"
	"time"

	"github.com/rotector/rotector/internal/common/database"
	"go.uber.org/zap"
)

// Session represents a user's session.
type Session struct {
	db           *database.Database
	lastActivity time.Time
	data         map[string]interface{}
	logger       *zap.Logger
	mu           sync.RWMutex
}

// NewSession creates a new session for the given user.
func NewSession(db *database.Database, logger *zap.Logger) *Session {
	return &Session{
		db:           db,
		lastActivity: time.Now(),
		data:         make(map[string]interface{}),
		logger:       logger,
		mu:           sync.RWMutex{},
	}
}

// Get returns the value for the given key.
func (s *Session) Get(key string) interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if value, ok := s.data[key]; ok {
		return value
	}

	s.logger.Warn("Session key not found", zap.String("key", key))
	return nil
}

// Set sets the value for the given key.
func (s *Session) Set(key string, value interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data[key] = value
	s.logger.Debug("Session key set", zap.String("key", key))
}

// Delete deletes the value for the given key.
func (s *Session) Delete(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()

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

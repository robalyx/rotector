package session

import (
	"sync"
	"time"

	"github.com/disgoorg/snowflake/v2"
	"github.com/rotector/rotector/internal/common/database"
)

const (
	KeyTarget         = "target"
	KeySortBy         = "sortBy"
	KeyPaginationPage = "paginationPage"
)

// Session represents a user's session.
type Session struct {
	db           *database.Database
	userID       snowflake.ID
	lastActivity time.Time
	data         map[string]interface{}
	mu           sync.RWMutex
}

// NewSession creates a new session for the given user.
func NewSession(db *database.Database, userID snowflake.ID) *Session {
	return &Session{
		db:           db,
		userID:       userID,
		lastActivity: time.Now(),
		data:         make(map[string]interface{}),
		mu:           sync.RWMutex{},
	}
}

// UserID returns the session's user ID.
func (s *Session) UserID() snowflake.ID {
	return s.userID
}

// LastActivity returns the session's last activity time.
func (s *Session) LastActivity() time.Time {
	return s.lastActivity
}

// UpdateLastActivity updates the session's last activity time.
func (s *Session) UpdateLastActivity() {
	s.lastActivity = time.Now()
}

// Get returns the value for the given key.
func (s *Session) Get(key string) interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.data[key]
}

// Set sets the value for the given key.
func (s *Session) Set(key string, value interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data[key] = value
}

// Delete deletes the value for the given key.
func (s *Session) Delete(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.data, key)
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

// GetPendingUser returns the pending user for the given key.
func (s *Session) GetPendingUser(key string) *database.PendingUser {
	if value, ok := s.Get(key).(*database.PendingUser); ok {
		return value
	}
	return nil
}

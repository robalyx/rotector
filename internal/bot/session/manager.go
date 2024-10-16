package session

import (
	"sync"
	"time"

	"github.com/disgoorg/snowflake/v2"
	"github.com/rotector/rotector/internal/common/database"
)

const (
	SessionTimeout = 15 * time.Minute
)

// Manager manages the sessions for the bot.
type Manager struct {
	db       *database.Database
	sessions map[snowflake.ID]*Session
	mu       sync.RWMutex
}

// NewManager creates a new session manager.
func NewManager(db *database.Database) *Manager {
	manager := &Manager{
		db:       db,
		sessions: make(map[snowflake.ID]*Session),
	}
	go manager.cleanupSessions()
	return manager
}

// GetOrCreateSession gets the session for the given user ID, or creates a new one if it doesn't exist.
func (m *Manager) GetOrCreateSession(userID snowflake.ID) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if the session already exists
	if session, exists := m.sessions[userID]; exists {
		session.UpdateLastActivity()
		return session
	}

	// Otherwise, create a new session
	session := NewSession(m.db, userID)
	m.sessions[userID] = session
	return session
}

// CloseSession closes the session for the given user ID.
func (m *Manager) CloseSession(userID snowflake.ID) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.sessions, userID)
}

// cleanupSessions cleans up the sessions that have not been active for a long time.
func (m *Manager) cleanupSessions() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	// Cleanup sessions every 5 minutes
	for range ticker.C {
		m.mu.Lock()
		for userID, session := range m.sessions {
			if time.Since(session.LastActivity()) > SessionTimeout {
				delete(m.sessions, userID)
			}
		}
		m.mu.Unlock()
	}
}

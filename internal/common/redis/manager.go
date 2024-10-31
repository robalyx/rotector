package redis

import (
	"fmt"
	"sync"

	"github.com/redis/rueidis"
	"github.com/rotector/rotector/internal/common/config"
	"go.uber.org/zap"
)

const (
	CacheDBIndex   = 0
	StatsDBIndex   = 1
	QueueDBIndex   = 2
	SessionDBIndex = 3
)

// Manager handles Redis client management.
type Manager struct {
	clients map[int]rueidis.Client
	config  *config.Config
	logger  *zap.Logger
	mu      sync.RWMutex
}

// NewManager creates a new Redis manager instance.
func NewManager(config *config.Config, logger *zap.Logger) *Manager {
	return &Manager{
		clients: make(map[int]rueidis.Client),
		config:  config,
		logger:  logger,
	}
}

// GetClient returns a Redis client for the given database index.
// If the client doesn't exist, it creates a new one.
func (m *Manager) GetClient(dbIndex int) (rueidis.Client, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if client, exists := m.clients[dbIndex]; exists {
		return client, nil
	}

	// Create new client if it doesn't exist
	client, err := rueidis.NewClient(rueidis.ClientOption{
		InitAddress: []string{fmt.Sprintf("%s:%d", m.config.Redis.Host, m.config.Redis.Port)},
		Username:    m.config.Redis.Username,
		Password:    m.config.Redis.Password,
		SelectDB:    dbIndex,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create Redis client for DB %d: %w", dbIndex, err)
	}

	m.clients[dbIndex] = client
	m.logger.Info("Created new Redis client", zap.Int("dbIndex", dbIndex))
	return client, nil
}

// Close closes all Redis clients.
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for dbIndex, client := range m.clients {
		client.Close()
		m.logger.Info("Closed Redis client", zap.Int("dbIndex", dbIndex))
	}
}

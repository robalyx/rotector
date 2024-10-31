package session

import (
	"context"
	"fmt"
	"time"

	"github.com/bytedance/sonic"
	"github.com/disgoorg/snowflake/v2"
	"github.com/maypok86/otter"
	"github.com/redis/rueidis"
	"github.com/rotector/rotector/internal/common/database"
	"github.com/rotector/rotector/internal/common/redis"
	"go.uber.org/zap"
)

const (
	SessionTimeout = 10 * time.Minute
	SessionPrefix  = "session:"

	LocalCacheSize = 10000
	ScanBatchSize  = 1000
)

// Manager manages the sessions for the bot.
type Manager struct {
	db     *database.Database
	redis  rueidis.Client
	cache  otter.Cache[string, *Session]
	logger *zap.Logger
}

// NewManager creates a new session manager.
func NewManager(db *database.Database, redisManager *redis.Manager, logger *zap.Logger) (*Manager, error) {
	// Get Redis client for sessions
	redisClient, err := redisManager.GetClient(redis.SessionDBIndex)
	if err != nil {
		return nil, fmt.Errorf("failed to get Redis client: %w", err)
	}

	// Initialize local cache
	cache, err := otter.MustBuilder[string, *Session](LocalCacheSize).
		WithTTL(SessionTimeout).
		Build()
	if err != nil {
		return nil, fmt.Errorf("failed to create local cache: %w", err)
	}

	return &Manager{
		db:     db,
		redis:  redisClient,
		logger: logger,
		cache:  cache,
	}, nil
}

// Close closes the session manager and its resources.
func (m *Manager) Close() {
	m.cache.Close()
}

// GetOrCreateSession gets the session for the given user ID, or creates a new one if it doesn't exist.
func (m *Manager) GetOrCreateSession(ctx context.Context, userID snowflake.ID) *Session {
	key := fmt.Sprintf("%s%s", SessionPrefix, userID)

	// Try to get from local cache first
	if session, ok := m.cache.Get(key); ok {
		session.Touch(ctx)
		return session
	}

	// Try to get existing session from Redis
	result := m.redis.Do(ctx, m.redis.B().Get().Key(key).Build())
	if result.Error() == nil {
		// Session exists, deserialize it
		var sessionData map[string]interface{}
		if data, err := result.AsBytes(); err == nil {
			if err := sonic.Unmarshal(data, &sessionData); err == nil {
				session := NewSession(m.db, m.redis, key, m.logger)
				session.data = sessionData
				session.Touch(ctx)

				m.cache.Set(key, session)
				return session
			}
		}
	}

	// Create new session if it doesn't exist or couldn't be deserialized
	session := NewSession(m.db, m.redis, key, m.logger)
	session.Touch(ctx)

	m.cache.Set(key, session)
	return session
}

// CloseSession closes the session for the given user ID.
func (m *Manager) CloseSession(ctx context.Context, userID snowflake.ID) {
	key := fmt.Sprintf("%s%s", SessionPrefix, userID)

	// Remove from local cache
	m.cache.Delete(key)

	// Remove from Redis
	if err := m.redis.Do(ctx, m.redis.B().Del().Key(key).Build()).Error(); err != nil {
		m.logger.Error("Failed to delete session", zap.Error(err))
	}
}

// GetActiveUsers returns a list of user IDs with active sessions.
func (m *Manager) GetActiveUsers(ctx context.Context) []snowflake.ID {
	pattern := SessionPrefix + "*"
	var activeUsers []snowflake.ID
	cursor := uint64(0)

	for {
		// Use SCAN to iterate through keys
		result := m.redis.Do(ctx, m.redis.B().Scan().Cursor(cursor).Match(pattern).Count(ScanBatchSize).Build())
		if result.Error() != nil {
			m.logger.Error("Failed to scan Redis keys", zap.Error(result.Error()))
			return nil
		}

		keys, err := result.AsScanEntry()
		if err != nil {
			m.logger.Error("Failed to get scan entry", zap.Error(err))
			return nil
		}

		// Process each key from the scan batch
		for _, key := range keys.Elements {
			userIDStr := key[len(SessionPrefix):]
			if userID, err := snowflake.Parse(userIDStr); err == nil {
				activeUsers = append(activeUsers, userID)
			}
		}

		if keys.Cursor == 0 {
			break
		}
		cursor = keys.Cursor
	}

	return activeUsers
}

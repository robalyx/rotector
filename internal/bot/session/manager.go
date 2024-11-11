package session

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/bytedance/sonic"
	"github.com/disgoorg/snowflake/v2"
	"github.com/redis/rueidis"
	"github.com/rotector/rotector/internal/bot/constants"
	"github.com/rotector/rotector/internal/common/database"
	"github.com/rotector/rotector/internal/common/redis"
	"go.uber.org/zap"
)

const (
	// SessionTimeout defines how long a session remains valid before expiring.
	// After this duration, Redis will automatically delete the session data.
	SessionTimeout = 10 * time.Minute

	// SessionPrefix is prepended to all session keys in Redis to namespace them
	// and avoid conflicts with other data stored in the same Redis instance.
	SessionPrefix = "session:"

	// ScanBatchSize controls how many Redis keys are retrieved in each SCAN operation
	// when listing active sessions. This helps balance memory usage and performance.
	ScanBatchSize = 1000
)

// Manager manages the session lifecycle using Redis as the backing store.
// Sessions are prefixed and stored with automatic expiration.
type Manager struct {
	db     *database.Database
	redis  rueidis.Client
	logger *zap.Logger
}

// NewManager creates a new session manager that uses Redis as the backing store.
func NewManager(db *database.Database, redisManager *redis.Manager, logger *zap.Logger) (*Manager, error) {
	redisClient, err := redisManager.GetClient(redis.SessionDBIndex)
	if err != nil {
		return nil, fmt.Errorf("failed to get Redis client: %w", err)
	}

	return &Manager{
		db:     db,
		redis:  redisClient,
		logger: logger,
	}, nil
}

// GetOrCreateSession loads or initializes a session for a given user.
// New sessions are populated with user settings from the database.
// Existing sessions are refreshed with the latest user settings.
func (m *Manager) GetOrCreateSession(ctx context.Context, userID snowflake.ID) (*Session, error) {
	key := fmt.Sprintf("%s%s", SessionPrefix, userID)

	// Load user settings
	settings, err := m.db.Settings().GetUserSettings(ctx, uint64(userID))
	if err != nil {
		return nil, fmt.Errorf("failed to load user settings: %w", err)
	}

	// Try loading existing session
	result := m.redis.Do(ctx, m.redis.B().Get().Key(key).Build())
	if err := result.Error(); err != nil {
		if errors.Is(err, rueidis.Nil) {
			// Initialize new session with fresh settings
			sessionData := make(map[string]interface{})
			session := NewSession(m.db, m.redis, key, sessionData, m.logger)
			session.Set(constants.SessionKeyUserSettings, settings)
			return session, nil
		}
		return nil, fmt.Errorf("failed to query Redis: %w", err)
	}

	// Deserialize existing session
	data, err := result.AsBytes()
	if err != nil {
		return nil, fmt.Errorf("failed to get session data as bytes: %w", err)
	}

	var sessionData map[string]interface{}
	if err := sonic.Unmarshal(data, &sessionData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal session data: %w", err)
	}

	// Update existing session with latest settings
	session := NewSession(m.db, m.redis, key, sessionData, m.logger)
	session.Set(constants.SessionKeyUserSettings, settings)
	return session, nil
}

// CloseSession removes a user's session from Redis immediately rather than
// waiting for expiration.
func (m *Manager) CloseSession(ctx context.Context, userID snowflake.ID) {
	key := fmt.Sprintf("%s%s", SessionPrefix, userID)
	if err := m.redis.Do(ctx, m.redis.B().Del().Key(key).Build()).Error(); err != nil {
		m.logger.Error("Failed to delete session", zap.Error(err))
	}
}

// GetActiveUsers scans Redis for all session keys and extracts the user IDs.
// Uses cursor-based scanning to handle large numbers of sessions.
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

		// Extract user IDs from key names
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

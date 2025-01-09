package session

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/bytedance/sonic"
	"github.com/disgoorg/snowflake/v2"
	"github.com/redis/rueidis"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/common/storage/database"
	"github.com/robalyx/rotector/internal/common/storage/redis"
	"go.uber.org/zap"
)

const (
	// BotSettingsRefreshInterval defines how often bot settings should be reloaded.
	BotSettingsRefreshInterval = time.Minute

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

// Session errors.
var (
	ErrSessionLimitReached  = errors.New("session limit reached")
	ErrFailedToGetCount     = errors.New("failed to get active session count")
	ErrFailedToLoadSettings = errors.New("failed to load settings")
	ErrFailedToGetSession   = errors.New("failed to get session data")
	ErrFailedToParseSession = errors.New("failed to parse session data")
)

// Manager manages the session lifecycle using Redis as the backing store.
// Sessions are prefixed and stored with automatic expiration.
type Manager struct {
	db     *database.Client
	redis  rueidis.Client
	logger *zap.Logger
}

// NewManager creates a new session manager that uses Redis as the backing store.
func NewManager(db *database.Client, redisManager *redis.Manager, logger *zap.Logger) (*Manager, error) {
	// Get Redis client
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
	// Load bot settings
	botSettings, err := m.db.Settings().GetBotSettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrFailedToLoadSettings, err)
	}

	// Try loading existing session first
	key := fmt.Sprintf("%s%d", SessionPrefix, userID)
	result := m.redis.Do(ctx, m.redis.B().Get().Key(key).Build())
	sessionExists := result.Error() == nil

	// If session doesn't exist, check session limit
	if !sessionExists && botSettings.SessionLimit > 0 {
		activeCount, err := m.GetActiveSessionCount(ctx)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrFailedToGetCount, err)
		}

		if activeCount >= botSettings.SessionLimit {
			m.logger.Debug("Session limit reached",
				zap.Uint64("active_count", activeCount),
				zap.Uint64("limit", botSettings.SessionLimit))
			return nil, ErrSessionLimitReached
		}
	}

	// If session exists, update it
	if sessionExists {
		data, err := result.AsBytes()
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrFailedToGetSession, err)
		}

		var sessionData map[string]interface{}
		if err := sonic.Unmarshal(data, &sessionData); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrFailedToParseSession, err)
		}

		session := NewSession(m.db, m.redis, key, sessionData, m.logger, uint64(userID))
		session.Set(constants.SessionKeyBotSettings, botSettings)
		return session, nil
	}

	// Load user settings
	userSettings, err := m.db.Settings().GetUserSettings(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrFailedToLoadSettings, err)
	}

	// Initialize new session with fresh settings
	sessionData := make(map[string]interface{})
	session := NewSession(m.db, m.redis, key, sessionData, m.logger, uint64(userID))
	session.Set(constants.SessionKeyUserSettings, userSettings)
	session.Set(constants.SessionKeyBotSettings, botSettings)
	return session, nil
}

// CloseSession removes a user's session from Redis immediately rather than
// waiting for expiration.
func (m *Manager) CloseSession(ctx context.Context, userID uint64) {
	key := fmt.Sprintf("%s%d", SessionPrefix, userID)
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

// GetActiveSessionCount returns the number of active sessions.
func (m *Manager) GetActiveSessionCount(ctx context.Context) (uint64, error) {
	pattern := SessionPrefix + "*"
	count := uint64(0)
	cursor := uint64(0)

	for {
		// Use SCAN to iterate through keys
		result := m.redis.Do(ctx, m.redis.B().Scan().Cursor(cursor).Match(pattern).Count(ScanBatchSize).Build())
		if result.Error() != nil {
			return 0, fmt.Errorf("failed to scan Redis keys: %w", result.Error())
		}

		keys, err := result.AsScanEntry()
		if err != nil {
			return 0, fmt.Errorf("failed to get scan entry: %w", err)
		}

		count += uint64(len(keys.Elements))

		if keys.Cursor == 0 {
			break
		}
		cursor = keys.Cursor
	}

	return count, nil
}

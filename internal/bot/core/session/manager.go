package session

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/bytedance/sonic"
	"github.com/disgoorg/snowflake/v2"
	"github.com/redis/rueidis"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/database"
	"github.com/robalyx/rotector/internal/redis"
	"go.uber.org/zap"
)

const (
	// SessionPrefix is prepended to all session keys in Redis to namespace them
	// and avoid conflicts with other data stored in the same Redis instance.
	SessionPrefix = "session:"

	// ScanBatchSize controls how many Redis keys are retrieved in each SCAN operation
	// when listing active sessions. This helps balance memory usage and performance.
	ScanBatchSize = 1000

	// MaxSessionsPerUser is the maximum number of sessions allowed per user.
	MaxSessionsPerUser = 3
)

var (
	ErrSessionLimitReached  = errors.New("session limit reached")
	ErrSessionNotFound      = errors.New("session not found")
	ErrFailedToGetCount     = errors.New("failed to get active session count")
	ErrFailedToLoadSettings = errors.New("failed to load settings")
	ErrFailedToGetSession   = errors.New("failed to get session data")
	ErrFailedToParseSession = errors.New("failed to parse session data")
)

// Info contains information about a user's session.
type Info struct {
	MessageID uint64    `json:"messageId"`
	PageName  string    `json:"pageName"`
	LastUsed  time.Time `json:"lastUsed"`
}

// Manager manages the session lifecycle using Redis as the backing store.
// Sessions are prefixed and stored with automatic expiration.
type Manager struct {
	db     database.Client
	redis  rueidis.Client
	logger *zap.Logger
}

// NewManager creates a new session manager that uses Redis as the backing store.
func NewManager(db database.Client, redisManager *redis.Manager, logger *zap.Logger) (*Manager, error) {
	// Get Redis client
	redisClient, err := redisManager.GetClient(redis.SessionDBIndex)
	if err != nil {
		return nil, fmt.Errorf("failed to get Redis client: %w", err)
	}

	return &Manager{
		db:     db,
		redis:  redisClient,
		logger: logger.Named("session_manager"),
	}, nil
}

// GetUserSessions retrieves all active sessions for a given user.
// If cleanupSelector is true, it will remove sessions stuck in the selector page.
func (m *Manager) GetUserSessions(ctx context.Context, userID uint64, cleanupSelector bool) ([]Info, error) {
	pattern := fmt.Sprintf("%s%d:*", SessionPrefix, userID)
	sessions := make([]Info, 0)
	cursor := uint64(0)

	for {
		result := m.redis.Do(ctx, m.redis.B().Scan().Cursor(cursor).Match(pattern).Count(ScanBatchSize).Build())
		if result.Error() != nil {
			return nil, fmt.Errorf("failed to scan Redis keys: %w", result.Error())
		}

		keys, err := result.AsScanEntry()
		if err != nil {
			return nil, fmt.Errorf("failed to get scan entry: %w", err)
		}

		// Get data for each session
		for _, key := range keys.Elements {
			// Extract message ID from key (format: session:{userID}:{messageID})
			parts := strings.Split(key, ":")
			if len(parts) != 3 {
				m.logger.Debug("Invalid key format", zap.String("key", key))
				continue
			}

			// Parse message ID from key
			messageID, err := strconv.ParseUint(parts[2], 10, 64)
			if err != nil {
				m.logger.Debug("Failed to parse message ID",
					zap.String("key", key),
					zap.Error(err))
				continue
			}

			// Get session data from Redis
			data := m.redis.Do(ctx, m.redis.B().Get().Key(key).Build())
			if data.Error() != nil {
				m.logger.Debug("Failed to get session data",
					zap.String("key", key),
					zap.Error(data.Error()))
				continue
			}

			bytes, err := data.AsBytes()
			if err != nil {
				m.logger.Debug("Failed to get bytes",
					zap.String("key", key),
					zap.Error(err))
				continue
			}

			// Unmarshal session data
			var sessionData map[string]any
			if err := sonic.Unmarshal(bytes, &sessionData); err != nil {
				m.logger.Debug("Failed to unmarshal session data",
					zap.String("key", key),
					zap.Error(err))
				continue
			}

			// Create temporary session to access the data
			tempSession := &Session{data: sessionData}
			pageName := CurrentPage.Get(tempSession)

			// Only delete selector sessions if cleanupSelector is true
			if cleanupSelector && pageName == constants.SessionSelectorPageName {
				if err := m.redis.Do(ctx, m.redis.B().Del().Key(key).Build()).Error(); err != nil {
					m.logger.Error("Failed to delete selector session",
						zap.Error(err),
						zap.String("key", key))
				}
				m.logger.Debug("Removed session in selector page",
					zap.Uint64("user_id", userID),
					zap.Uint64("message_id", messageID))
				continue
			}

			sessions = append(sessions, Info{
				MessageID: messageID,
				PageName:  pageName,
				LastUsed:  LastUsed.Get(tempSession),
			})
		}

		if keys.Cursor == 0 {
			break
		}
		cursor = keys.Cursor
	}

	return sessions, nil
}

// GetOrCreateSession retrieves or creates a session for the given user and message.
func (m *Manager) GetOrCreateSession(
	ctx context.Context, userID snowflake.ID, messageID uint64, isGuildOwner, checkOutdated bool,
) (*Session, bool, error) {
	// Load bot settings
	botSettings, err := m.db.Model().Setting().GetBotSettings(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("%w: %w", ErrFailedToLoadSettings, err)
	}

	// Generate session key
	key := fmt.Sprintf("%s%d:%d", SessionPrefix, userID, messageID)

	// Try loading existing session first
	result := m.redis.Do(ctx, m.redis.B().Get().Key(key).Build())
	sessionExists := result.Error() == nil

	if !sessionExists {
		// If the session doesn't exist, it means the message is outdated
		if checkOutdated {
			return nil, false, ErrSessionNotFound
		}

		// Get all existing sessions for this user
		sessions, err := m.GetUserSessions(ctx, uint64(userID), true)
		if err != nil {
			return nil, false, fmt.Errorf("failed to get user sessions: %w", err)
		}

		isAdmin := botSettings.IsAdmin(uint64(userID))

		// Check global session limit unless user is admin
		if !isAdmin && botSettings.SessionLimit > 0 {
			activeCount, err := m.GetActiveSessionCount(ctx)
			if err != nil {
				return nil, false, fmt.Errorf("failed to get active session count: %w", err)
			}

			if activeCount >= botSettings.SessionLimit {
				m.logger.Debug("Global session limit reached",
					zap.Uint64("active_count", activeCount),
					zap.Uint64("limit", botSettings.SessionLimit))
				return nil, false, ErrSessionLimitReached
			}
		}

		// If user has reached their session limit, remove the oldest session
		if !isAdmin && len(sessions) > MaxSessionsPerUser {
			// Sort sessions by LastUsed time
			sort.Slice(sessions, func(i, j int) bool {
				return sessions[i].LastUsed.Before(sessions[j].LastUsed)
			})

			// Remove the oldest session
			oldestSession := sessions[0]
			oldestKey := fmt.Sprintf("%s%d:%d", SessionPrefix, userID, oldestSession.MessageID)
			if err := m.redis.Do(ctx, m.redis.B().Del().Key(oldestKey).Build()).Error(); err != nil {
				m.logger.Error("Failed to delete oldest session",
					zap.Error(err),
					zap.String("key", oldestKey))
			}

			m.logger.Debug("Removed oldest session",
				zap.Uint64("user_id", uint64(userID)),
				zap.Uint64("message_id", oldestSession.MessageID),
				zap.Time("last_used", oldestSession.LastUsed))
		}
	}

	// Load user settings
	userSettings, err := m.db.Model().Setting().GetUserSettings(ctx, userID)
	if err != nil {
		return nil, false, fmt.Errorf("%w: %w", ErrFailedToLoadSettings, err)
	}

	// If session exists, update it
	if sessionExists {
		data, err := result.AsBytes()
		if err != nil {
			return nil, false, fmt.Errorf("%w: %w", ErrFailedToGetSession, err)
		}

		var sessionData map[string]any
		if err := sonic.Unmarshal(data, &sessionData); err != nil {
			return nil, false, fmt.Errorf("%w: %w", ErrFailedToParseSession, err)
		}

		session := NewSession(userSettings, botSettings, m.db, m.redis, key, sessionData, m.logger)
		UserID.Set(session, uint64(userID))
		MessageID.Set(session, messageID)
		IsGuildOwner.Set(session, isGuildOwner)
		return session, false, nil
	}

	// Initialize new session
	session := NewSession(userSettings, botSettings, m.db, m.redis, key, make(map[string]any), m.logger)
	UserID.Set(session, uint64(userID))
	MessageID.Set(session, messageID)
	IsGuildOwner.Set(session, isGuildOwner)
	return session, true, nil
}

// CloseSession removes a user's session from Redis immediately rather than
// waiting for expiration.
func (m *Manager) CloseSession(ctx context.Context, session *Session, userID uint64, messageID uint64) {
	key := fmt.Sprintf("%s%d:%d", SessionPrefix, userID, messageID)
	if err := m.redis.Do(ctx, m.redis.B().Del().Key(key).Build()).Error(); err != nil {
		m.logger.Error("Failed to delete session",
			zap.Error(err),
			zap.String("key", key))
	}

	if session != nil {
		session.Close()
	}
}

// GetActiveUsers scans Redis for all session keys and extracts the user IDs.
// Uses cursor-based scanning to handle large numbers of sessions.
func (m *Manager) GetActiveUsers(ctx context.Context) []uint64 {
	pattern := SessionPrefix + "*"
	var activeUsers []uint64
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
			if userID, err := strconv.ParseUint(userIDStr, 10, 64); err == nil {
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

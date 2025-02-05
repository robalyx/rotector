//go:generate go run settings_gen.go
//go:generate go run keys_gen.go

package session

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"sync"

	"github.com/redis/rueidis"

	"github.com/bytedance/sonic"
	"github.com/robalyx/rotector/internal/common/storage/database"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"go.uber.org/zap"
)

// Session maintains user state through a Redis-backed key-value store where values are
// serialized as JSON strings. The session automatically expires after a configured timeout.
type Session struct {
	userSettings       *types.UserSetting
	botSettings        *types.BotSetting
	userSettingsUpdate bool
	botSettingsUpdate  bool
	db                 database.Client
	redis              rueidis.Client
	key                string
	data               map[string]interface{}
	dataModified       map[string]bool
	mu                 sync.RWMutex
	logger             *zap.Logger
	userID             uint64
}

// NewSession creates a new session for the given user.
func NewSession(
	userSettings *types.UserSetting,
	botSettings *types.BotSetting,
	db database.Client,
	redis rueidis.Client,
	key string,
	data map[string]interface{},
	logger *zap.Logger,
	userID uint64,
) *Session {
	return &Session{
		userSettings:       userSettings,
		botSettings:        botSettings,
		userSettingsUpdate: false,
		botSettingsUpdate:  false,
		db:                 db,
		redis:              redis,
		key:                key,
		data:               data,
		dataModified:       make(map[string]bool),
		logger:             logger,
		userID:             userID,
	}
}

// UserID returns the user ID associated with the session.
func (s *Session) UserID() uint64 {
	return s.userID
}

// Touch serializes the session data to JSON and updates the TTL in Redis to prevent expiration.
// If serialization fails, the error is logged but the session continues.
func (s *Session) Touch(ctx context.Context) {
	// Create a map of only persistent data
	persistentData := make(map[string]interface{})
	s.mu.RLock()
	for key, value := range s.data {
		isPersistent, ok := s.dataModified[key]
		if !ok || (ok && isPersistent) {
			persistentData[key] = value
		}
	}
	s.mu.RUnlock()

	// Serialize only persistent data to JSON
	data, err := sonic.MarshalString(persistentData)
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

	// Only save user settings if they've been updated
	if s.userSettingsUpdate {
		if err := s.db.Models().Settings().SaveUserSettings(ctx, s.userSettings); err != nil {
			s.logger.Error("Failed to save user settings", zap.Error(err))
			return
		}
		s.userSettingsUpdate = false
	}

	// Only save bot settings if they've been updated
	if s.botSettings.IsAdmin(s.userID) && s.botSettingsUpdate {
		if err := s.db.Models().Settings().SaveBotSettings(ctx, s.botSettings); err != nil {
			s.logger.Error("Failed to save bot settings", zap.Error(err))
			return
		}
		s.botSettingsUpdate = false
	}
}

// UserSettings returns the current user settings.
func (s *Session) UserSettings() *types.UserSetting {
	return s.userSettings
}

// BotSettings returns the current bot settings.
func (s *Session) BotSettings() *types.BotSetting {
	return s.botSettings
}

// get retrieves a raw string value from the in-memory session cache.
// Returns empty string if key doesn't exist.
func (s *Session) get(key string) interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if value, ok := s.data[key]; ok {
		return value
	}
	s.logger.Debug("Session key not found", zap.String("key", key))
	return nil
}

// getInterface unmarshals the stored value into the provided interface.
func (s *Session) getInterface(key string, v interface{}) {
	value := s.get(key)
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

// getBuffer retrieves and decodes a base64 encoded buffer from the session.
func (s *Session) getBuffer(key string) *bytes.Buffer {
	value := s.get(key)
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

// set sets the value for the given key.
func (s *Session) set(key string, value interface{}, persist bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data[key] = value
	s.dataModified[key] = persist

	s.logger.Debug("Session key set", zap.String("key", key))
}

// setBuffer stores binary data by base64 encoding it first.
// This allows binary data to be safely stored as strings in the session.
func (s *Session) setBuffer(key string, buf *bytes.Buffer, persist bool) {
	if buf == nil {
		s.logger.Warn("Attempted to set nil buffer", zap.String("key", key))
		return
	}

	// Encode buffer to base64
	encoded := base64.StdEncoding.EncodeToString(buf.Bytes())

	s.mu.Lock()
	defer s.mu.Unlock()

	s.data[key] = encoded
	s.dataModified[key] = persist

	s.logger.Debug("Session key set with base64 encoded buffer", zap.String("key", key))
}

// delete removes a key from the session data.
func (s *Session) delete(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.data, key)
	s.logger.Debug("Session key deleted", zap.String("key", key))
}

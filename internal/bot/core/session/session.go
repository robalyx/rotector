//go:generate go run settings_gen.go
//go:generate go run keys_gen.go

package session

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"sync"
	"time"

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
	}
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
			persistentData[key] = processValue(value)
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
	if s.botSettings.IsAdmin(UserID.Get(s)) && s.botSettingsUpdate {
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

	// Special handling for primitive types that need string conversion
	strValue, isString := value.(string)
	if isString {
		switch typedPtr := v.(type) {
		case *uint64:
			// Handle uint64 values stored as strings
			parsedValue, err := strconv.ParseUint(strValue, 10, 64)
			if err == nil {
				*typedPtr = parsedValue
				return
			}
			s.logger.Error("Failed to parse uint64 from string",
				zap.Error(err),
				zap.String("key", key),
				zap.String("value", strValue))

		case *time.Time:
			// Handle time.Time values stored as strings
			parsedTime, err := time.Parse(time.RFC3339Nano, strValue)
			if err == nil {
				*typedPtr = parsedTime
				return
			}
			s.logger.Error("Failed to parse time.Time from string",
				zap.Error(err),
				zap.String("key", key),
				zap.String("value", strValue))
		}
	}

	// DEVELOPER NOTE:
	// This double marshal/unmarshal process is necessary to handle nested uint64 values with full precision.
	//
	// THE PROBLEM:
	// Standard JSON unmarshaling in Go treats all numbers as float64 by default. This causes precision loss
	// for uint64 values that exceed float64's exact integer representation limit (~2^53). For example, the
	// uint64 value 18446744073709551615 would lose precision if converted to float64.
	//
	// OUR SOLUTION:
	// 1. When storing in Redis: We convert uint64 values to strings to preserve their exact value
	// 2. When retrieving simple values: The switch statement above handles direct string-to-uint64 conversion
	// 3. For complex nested structures: We need the process below
	//
	// THE PROCESS:
	// 1. Marshal the value to JSON
	// 2. Unmarshal with decoder.UseNumber() to preserve numeric precision as json.Number
	//    (This avoids automatic float64 conversion that would lose precision)
	// 3. Recursively process the structure to convert json.Number and string representations to uint64
	// 4. Re-marshal the processed structure with proper types
	// 5. Unmarshal again into the target type with all precision preserved
	//
	// WHY DOUBLE MARSHALING:
	// We can't simply use decoder.UseNumber() when unmarshaling directly into the target type
	// because UseNumber only affects how numbers are stored in map[string]interface{} and []interface{}.
	// The preserveNumericPrecision function is needed to recursively convert these to actual uint64 values.

	// First marshal to JSON
	jsonBytes, err := sonic.Marshal(value)
	if err != nil {
		s.logger.Error("Failed to marshal interface",
			zap.Error(err),
			zap.String("key", key),
			zap.Any("value", value))
		return
	}

	// First unmarshal with number precision preservation
	var rawData interface{}
	decoder := sonic.ConfigStd.NewDecoder(bytes.NewReader(jsonBytes))
	decoder.UseNumber() // This ensures numbers are decoded as json.Number to preserve precision
	if err := decoder.Decode(&rawData); err != nil {
		s.logger.Error("Failed to unmarshal to raw data",
			zap.Error(err),
			zap.String("key", key))
		return
	}

	// Process the raw data to handle uint64 conversions
	processedData := preserveNumericPrecision(rawData)

	// Second marshal of the processed data
	processedBytes, err := sonic.Marshal(processedData)
	if err != nil {
		s.logger.Error("Failed to marshal processed data",
			zap.Error(err),
			zap.String("key", key))
		return
	}

	// Second unmarshal into the target interface
	if err := sonic.Unmarshal(processedBytes, v); err != nil {
		s.logger.Error("Failed to unmarshal interface",
			zap.Error(err),
			zap.String("key", key),
			zap.String("json", string(processedBytes)),
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

// processValue recursively processes values to convert uint64 to string.
func processValue(value interface{}) interface{} {
	if value == nil {
		return nil
	}

	// Direct uint64 conversion
	if uintValue, ok := value.(uint64); ok {
		return strconv.FormatUint(uintValue, 10)
	}

	// Handle time.Time values
	if timeValue, ok := value.(time.Time); ok {
		return timeValue.Format(time.RFC3339Nano)
	}

	// Handle slices
	refValue := reflect.ValueOf(value)
	switch refValue.Kind() {
	case reflect.Slice:
		if refValue.Type().Elem().Kind() == reflect.Uint64 {
			// Special case for []uint64
			result := make([]string, refValue.Len())
			for i := range refValue.Len() {
				result[i] = strconv.FormatUint(refValue.Index(i).Uint(), 10)
			}
			return result
		}
		// Process each element in the slice
		result := make([]interface{}, refValue.Len())
		for i := range refValue.Len() {
			if i < refValue.Len() {
				result[i] = processValue(refValue.Index(i).Interface())
			}
		}
		return result

	case reflect.Map:
		// Process map keys and values
		result := make(map[string]interface{})
		for _, key := range refValue.MapKeys() {
			// Convert map keys to strings
			var keyStr string
			if key.Kind() == reflect.Uint64 {
				keyStr = strconv.FormatUint(key.Uint(), 10)
			} else {
				keyStr = fmt.Sprintf("%v", key.Interface())
			}
			// Process map values
			result[keyStr] = processValue(refValue.MapIndex(key).Interface())
		}
		return result

	case reflect.Struct:
		// Special case for time.Time struct
		if t, ok := value.(time.Time); ok {
			return t.Format(time.RFC3339Nano)
		}

		// Process struct fields
		result := make(map[string]interface{})
		for i := range refValue.NumField() {
			field := refValue.Type().Field(i)
			if field.IsExported() {
				result[field.Name] = processValue(refValue.Field(i).Interface())
			}
		}
		return result

	case reflect.Ptr:
		if !refValue.IsNil() {
			return processValue(refValue.Elem().Interface())
		}
	} //exhaustive:ignore

	return value
}

// preserveNumericPrecision recursively processes a data structure and converts
// json.Number and numeric strings to uint64 where appropriate, maintaining
// precision for large integer values.
func preserveNumericPrecision(data interface{}) interface{} {
	switch v := data.(type) {
	case string:
		// Try to convert string to uint64 if it looks like a number
		if val, err := strconv.ParseUint(v, 10, 64); err == nil {
			return val
		}
		return v
	case json.Number:
		// Convert json.Number to uint64 if possible
		if val, err := v.Int64(); err == nil {
			if val >= 0 {
				return uint64(val)
			}
		}
		// If it can't be converted to Int64 (maybe too large) or is negative,
		// try parsing directly as uint64
		if val, err := strconv.ParseUint(v.String(), 10, 64); err == nil {
			return val
		}
		// If conversion to uint64 fails, try to preserve original number
		if f, err := v.Float64(); err == nil {
			return f
		}
		return v.String() // fallback to string representation
	case float64:
		// JSON unmarshal may convert large integers to float64, leading to precision loss
		// Convert to uint64 if it appears to be an integer
		if v == float64(uint64(v)) {
			return uint64(v)
		}
		return v
	case map[string]interface{}:
		// Process each map value recursively
		for k, item := range v {
			v[k] = preserveNumericPrecision(item)
		}
		return v
	case []interface{}:
		// Process each slice item recursively
		for i, item := range v {
			v[i] = preserveNumericPrecision(item)
		}
		return v
	default:
		return v
	}
}

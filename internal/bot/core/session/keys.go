package session

import (
	"bytes"
	"reflect"
	"strings"

	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"go.uber.org/zap"
)

// Key represents a strongly typed session key for storing arbitrary data.
type Key[T any] struct {
	name    string
	persist bool
}

// NewKey creates a new typed session key.
func NewKey[T any](name string, persist bool) Key[T] {
	return Key[T]{
		name:    name,
		persist: persist,
	}
}

// Get retrieves the value for this key.
func (k Key[T]) Get(s *Session) T {
	var value T
	s.getInterface(k.name, &value)
	return value
}

// Set stores the value for this key.
func (k Key[T]) Set(s *Session, value T) {
	s.set(k.name, value, k.persist)
}

// Delete removes the value for this key.
func (k Key[T]) Delete(s *Session) {
	s.delete(k.name)
}

// BufferKey represents a key for binary data.
type BufferKey struct {
	name    string
	persist bool
}

// NewBufferKey creates a new buffer key.
func NewBufferKey(name string, persist bool) BufferKey {
	return BufferKey{
		name:    name,
		persist: persist,
	}
}

// Get retrieves the buffer for this key.
func (k BufferKey) Get(s *Session) *bytes.Buffer {
	return s.getBuffer(k.name)
}

// Set stores the buffer for this key.
func (k BufferKey) Set(s *Session, value *bytes.Buffer) {
	s.setBuffer(k.name, value, k.persist)
}

// Delete removes the buffer for this key.
func (k BufferKey) Delete(s *Session) {
	s.delete(k.name)
}

// UserSettingKey represents a strongly typed key for user settings.
type UserSettingKey[T any] struct {
	name string
}

// NewUserSettingKey creates a new user setting key.
func NewUserSettingKey[T any](name string) UserSettingKey[T] {
	return UserSettingKey[T]{name: name}
}

// Set updates the user setting value and marks it for update.
func (k UserSettingKey[T]) Set(s *Session, value T) {
	if s.userSettings == nil {
		s.userSettings = &types.UserSetting{}
	}
	if getSettingField(s.userSettings, k.name, s.logger).setValue(value) {
		s.userSettingsUpdate = true // Mark settings for update
	}
}

// Get retrieves the user setting value.
func (k UserSettingKey[T]) Get(s *Session) T {
	var zero T
	if s.userSettings == nil {
		return zero
	}
	if val, ok := getSettingField(s.userSettings, k.name, s.logger).getValue(zero).(T); ok {
		return val
	}
	return zero
}

// BotSettingKey represents a strongly typed key for bot settings.
type BotSettingKey[T any] struct {
	name string
}

// NewBotSettingKey creates a new bot setting key.
func NewBotSettingKey[T any](name string) BotSettingKey[T] {
	return BotSettingKey[T]{name: name}
}

// Set updates the bot setting value and marks it for update.
func (k BotSettingKey[T]) Set(s *Session, value T) {
	if s.botSettings == nil {
		s.botSettings = &types.BotSetting{}
	}
	if getSettingField(s.botSettings, k.name, s.logger).setValue(value) {
		s.botSettingsUpdate = true // Mark settings for update
	}
}

// Get retrieves the bot setting value
func (k BotSettingKey[T]) Get(s *Session) T {
	var zero T
	if s.botSettings == nil {
		return zero
	}
	if val, ok := getSettingField(s.botSettings, k.name, s.logger).getValue(zero).(T); ok {
		return val
	}
	return zero
}

// settingField represents a field in a settings struct.
type settingField struct {
	value  reflect.Value // Parent struct value
	field  reflect.Value // Target field value
	name   string        // Field path
	logger *zap.Logger
}

// getSettingField safely gets a field from a settings struct using dot notation.
func getSettingField(settings any, fieldPath string, logger *zap.Logger) settingField {
	v := reflect.ValueOf(settings)
	if v.Kind() == reflect.Ptr {
		v = v.Elem() // Dereference pointer
	}

	parts := strings.Split(fieldPath, ".") // Split path into parts
	field := v
	for _, part := range parts {
		if !field.IsValid() {
			return settingField{logger: logger, name: fieldPath}
		}

		nextField := field.FieldByName(part)
		if !nextField.IsValid() {
			return settingField{logger: logger, name: fieldPath}
		}
		field = nextField

		if field.Kind() == reflect.Ptr {
			if field.IsNil() {
				field.Set(reflect.New(field.Type().Elem())) // Initialize nil pointer
			}
			field = field.Elem()
		}
	}

	return settingField{
		value:  v,
		field:  field,
		logger: logger,
		name:   fieldPath,
	}
}

// isValid checks if the field exists and logs an error if not.
func (f settingField) isValid() bool {
	if !f.field.IsValid() {
		f.logger.Error("Invalid setting field path", zap.String("field", f.name))
		return false
	}
	return true
}

// setValue safely sets a value to the field.
func (f settingField) setValue(value any) bool {
	if !f.isValid() {
		return false
	}

	// Check if the value is assignable to the field type
	val := reflect.ValueOf(value)
	if !val.Type().AssignableTo(f.field.Type()) {
		f.logger.Error("Invalid setting value type",
			zap.String("field", f.name),
			zap.String("expected", f.field.Type().String()),
			zap.String("got", val.Type().String()))
		return false
	}

	// Ensure the field is settable
	if !f.field.CanSet() {
		f.logger.Error("Field is not settable", zap.String("field", f.name))
		return false
	}

	f.field.Set(val)
	return true
}

// getValue safely gets a value from the field.
func (f settingField) getValue(zero any) any {
	if !f.isValid() {
		return zero
	}

	if !f.field.IsZero() {
		return f.field.Interface()
	}
	return zero
}

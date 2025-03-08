package session

import (
	"bytes"
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
	name   string
	getter func(*Session) T
	setter func(*Session, T)
}

// NewUserSettingKey creates a new user setting key.
func NewUserSettingKey[T any](name string, getter func(*Session) T, setter func(*Session, T)) UserSettingKey[T] {
	return UserSettingKey[T]{
		name:   name,
		getter: getter,
		setter: setter,
	}
}

// Set updates the user setting value.
func (k UserSettingKey[T]) Set(s *Session, value T) {
	k.setter(s, value)
}

// Get retrieves the user setting value.
func (k UserSettingKey[T]) Get(s *Session) T {
	return k.getter(s)
}

// BotSettingKey represents a strongly typed key for bot settings.
type BotSettingKey[T any] struct {
	name   string
	getter func(*Session) T
	setter func(*Session, T)
}

// NewBotSettingKey creates a new bot setting key.
func NewBotSettingKey[T any](name string, getter func(*Session) T, setter func(*Session, T)) BotSettingKey[T] {
	return BotSettingKey[T]{
		name:   name,
		getter: getter,
		setter: setter,
	}
}

// Set updates the bot setting value.
func (k BotSettingKey[T]) Set(s *Session, value T) {
	k.setter(s, value)
}

// Get retrieves the bot setting value.
func (k BotSettingKey[T]) Get(s *Session) T {
	return k.getter(s)
}

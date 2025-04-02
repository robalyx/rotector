package utils

import (
	"sync"
	"time"
)

// TTLMap provides a thread-safe map with expiring entries.
type TTLMap[K comparable, V any] struct {
	mu      sync.RWMutex
	data    map[K]V
	expires map[K]time.Time
	ttl     time.Duration
}

// NewTTLMap creates a new TTLMap with the specified TTL duration.
func NewTTLMap[K comparable, V any](ttl time.Duration) *TTLMap[K, V] {
	m := &TTLMap[K, V]{
		data:    make(map[K]V),
		expires: make(map[K]time.Time),
		ttl:     ttl,
	}

	go m.cleanup()

	return m
}

// Get retrieves a value from the map.
// Returns the value and whether it exists/is valid.
func (m *TTLMap[K, V]) Get(key K) (V, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	value, exists := m.data[key]
	if !exists {
		var zero V
		return zero, false
	}

	// Check if expired
	if time.Now().After(m.expires[key]) {
		var zero V
		return zero, false
	}

	return value, true
}

// Set adds or updates a value in the map.
func (m *TTLMap[K, V]) Set(key K, value V) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.data[key] = value
	m.expires[key] = time.Now().Add(m.ttl)
}

// Delete removes a key from the map.
func (m *TTLMap[K, V]) Delete(key K) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.data, key)
	delete(m.expires, key)
}

// cleanup periodically removes expired entries.
func (m *TTLMap[K, V]) cleanup() {
	ticker := time.NewTicker(m.ttl)
	defer ticker.Stop()

	for range ticker.C {
		m.mu.Lock()
		now := time.Now()
		for key, expires := range m.expires {
			if now.After(expires) {
				delete(m.data, key)
				delete(m.expires, key)
			}
		}
		m.mu.Unlock()
	}
}

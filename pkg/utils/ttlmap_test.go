package utils_test

import (
	"testing"
	"time"

	"github.com/robalyx/rotector/pkg/utils"
	"github.com/stretchr/testify/assert"
)

func TestTTLMap(t *testing.T) {
	t.Parallel()
	// Create a map with a short TTL for testing
	ttl := 100 * time.Millisecond
	m := utils.NewTTLMap[string, int](ttl)

	// Test Set and Get
	t.Run("basic set and get", func(t *testing.T) {
		t.Parallel()
		m.Set("test1", 123)
		value, exists := m.Get("test1")
		assert.True(t, exists)
		assert.Equal(t, 123, value)
	})

	// Test expiration
	t.Run("expiration", func(t *testing.T) {
		t.Parallel()
		m.Set("test2", 456)
		time.Sleep(ttl + 50*time.Millisecond) // Wait for expiration

		_, exists := m.Get("test2")
		assert.False(t, exists)
	})

	// Test Delete
	t.Run("delete", func(t *testing.T) {
		t.Parallel()
		m.Set("test3", 789)
		m.Delete("test3")
		_, exists := m.Get("test3")
		assert.False(t, exists)
	})

	// Test non-existent key
	t.Run("non-existent key", func(t *testing.T) {
		t.Parallel()

		_, exists := m.Get("nonexistent")
		assert.False(t, exists)
	})

	// Test updating existing key
	t.Run("update existing key", func(t *testing.T) {
		t.Parallel()
		m.Set("test4", 111)
		m.Set("test4", 222)
		value, exists := m.Get("test4")
		assert.True(t, exists)
		assert.Equal(t, 222, value)
	})

	// Test multiple types
	t.Run("different types", func(t *testing.T) {
		t.Parallel()

		stringMap := utils.NewTTLMap[string, string](ttl)
		stringMap.Set("hello", "world")
		value, exists := stringMap.Get("hello")
		assert.True(t, exists)
		assert.Equal(t, "world", value)
	})
}

func TestTTLMapConcurrent(t *testing.T) {
	t.Parallel()

	t.Run("concurrent access", func(t *testing.T) {
		t.Parallel()

		ttl := 100 * time.Millisecond
		m := utils.NewTTLMap[string, int](ttl)

		done := make(chan bool)

		go func() {
			for i := range 100 {
				m.Set("key", i)
			}

			done <- true
		}()

		go func() {
			for range 100 {
				m.Get("key")
			}

			done <- true
		}()

		// Wait for both goroutines to finish
		<-done
		<-done
	})
}

package ratelimit

import (
	"sync"
	"time"

	"github.com/robalyx/rotector/internal/common/utils"
	"go.uber.org/zap"
)

// Config contains configuration for rate limiting message events.
type Config struct {
	// PerUserLimit is the maximum number of messages to process per user per guild within the reset period.
	PerUserLimit int
	// PerUserResetPeriod is how often the per-user counters reset.
	PerUserResetPeriod time.Duration
	// PerGuildLimit is the maximum number of messages to process per guild within the reset period.
	PerGuildLimit int
	// PerGuildResetPeriod is how often the per-guild counters reset.
	PerGuildResetPeriod time.Duration
	// GlobalLimit is the maximum number of messages to process across all servers within the reset period.
	GlobalLimit int
	// GlobalResetPeriod is how often the global counters reset.
	GlobalResetPeriod time.Duration
}

// DefaultConfig returns a default rate limiting configuration.
func DefaultConfig() *Config {
	return &Config{
		PerUserLimit:        10,
		PerUserResetPeriod:  1 * time.Minute,
		PerGuildLimit:       500,
		PerGuildResetPeriod: 10 * time.Minute,
		GlobalLimit:         2500,
		GlobalResetPeriod:   10 * time.Minute,
	}
}

// Limiter implements rate limiting for message events.
type Limiter struct {
	config *Config
	logger *zap.Logger

	userCounters  *utils.TTLMap[uint64, int]
	guildCounters *utils.TTLMap[uint64, int]

	globalMu      sync.RWMutex
	globalCounter int
	globalResetAt time.Time
}

// New creates a new rate limiter with the provided configuration.
func New(config *Config, logger *zap.Logger) *Limiter {
	now := time.Now()
	return &Limiter{
		config:        config,
		logger:        logger.Named("ratelimit"),
		userCounters:  utils.NewTTLMap[uint64, int](config.PerUserResetPeriod),
		guildCounters: utils.NewTTLMap[uint64, int](config.PerGuildResetPeriod),
		globalCounter: 0,
		globalResetAt: now.Add(config.GlobalResetPeriod),
	}
}

// Allow checks if a message event should be allowed based on rate limits.
// Returns true if the event should be processed, false if it should be ignored.
func (l *Limiter) Allow(userID, guildID uint64) bool {
	if !l.allowUser(userID, guildID) {
		return false
	}

	if !l.allowGuild(guildID) {
		return false
	}

	if !l.allowGlobal() {
		return false
	}

	return true
}

// allowUser checks if a user is within their rate limits.
func (l *Limiter) allowUser(userID, guildID uint64) bool {
	// Create composite key from user and guild IDs
	key := (guildID << 32) | userID

	// Get current count
	count, exists := l.userCounters.Get(key)
	if !exists {
		count = 0
	}

	// Check if we're under the limit
	if count >= l.config.PerUserLimit {
		return false
	}

	// Increment the counter
	l.userCounters.Set(key, count+1)
	return true
}

// allowGuild checks if a guild is within its rate limits.
func (l *Limiter) allowGuild(guildID uint64) bool {
	// Get current count
	count, exists := l.guildCounters.Get(guildID)
	if !exists {
		count = 0
	}

	// Check if we're under the limit
	if count >= l.config.PerGuildLimit {
		return false
	}

	// Increment the counter
	l.guildCounters.Set(guildID, count+1)
	return true
}

// allowGlobal checks if we're within the global rate limits.
func (l *Limiter) allowGlobal() bool {
	now := time.Now()

	l.globalMu.Lock()
	defer l.globalMu.Unlock()

	// Check if we need to reset the counter
	if now.After(l.globalResetAt) {
		l.globalCounter = 0
		l.globalResetAt = now.Add(l.config.GlobalResetPeriod)
	}

	// Check if we're under the limit
	if l.globalCounter >= l.config.GlobalLimit {
		return false
	}

	// Increment the counter
	l.globalCounter++
	return true
}

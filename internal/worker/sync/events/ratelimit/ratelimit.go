package ratelimit

import (
	"sync"
	"time"

	"go.uber.org/zap"
)

// EventType defines the type of event for rate limiting purposes.
type EventType string

const (
	EventTypeMember   EventType = "member"
	EventTypeMessage  EventType = "message"
	EventTypeVoice    EventType = "voice"
	EventTypeTyping   EventType = "typing"
	EventTypeGuild    EventType = "guild"
	EventTypeUser     EventType = "user"
	EventTypePresence EventType = "presence"
)

// Config holds rate limiting configuration for different event types.
type Config struct {
	// PerUserCooldown is the minimum time between processing events from the same user in the same guild.
	PerUserCooldown map[EventType]time.Duration

	// PerGuildLimit is the maximum number of events to process per guild within the reset period.
	PerGuildLimit map[EventType]int

	// PerGuildResetPeriod is how often the per-guild counters reset.
	PerGuildResetPeriod time.Duration

	// GlobalLimit is the maximum number of events to process across all servers within the reset period.
	GlobalLimit map[EventType]int

	// GlobalResetPeriod is how often the global counters reset.
	GlobalResetPeriod time.Duration
}

// DefaultConfig returns a reasonable default configuration for rate limiting.
func DefaultConfig() *Config {
	return &Config{
		PerUserCooldown: map[EventType]time.Duration{
			EventTypeMember:   time.Hour,
			EventTypeMessage:  time.Hour * 6,
			EventTypeVoice:    time.Hour * 6,
			EventTypeTyping:   time.Hour * 6,
			EventTypeGuild:    time.Hour * 6,
			EventTypeUser:     time.Hour * 12,
			EventTypePresence: time.Hour * 12,
		},
		PerGuildLimit: map[EventType]int{
			EventTypeMember:   50,
			EventTypeMessage:  20,
			EventTypeVoice:    20,
			EventTypeTyping:   10,
			EventTypeGuild:    5,
			EventTypeUser:     10,
			EventTypePresence: 30,
		},
		PerGuildResetPeriod: 10 * time.Minute,
		GlobalLimit: map[EventType]int{
			EventTypeMember:   500,
			EventTypeMessage:  200,
			EventTypeVoice:    200,
			EventTypeTyping:   100,
			EventTypeGuild:    50,
			EventTypeUser:     10,
			EventTypePresence: 300,
		},
		GlobalResetPeriod: 1 * time.Minute,
	}
}

// Limiter provides rate limiting functionality for Discord events.
type Limiter struct {
	config *Config
	logger *zap.Logger

	userMu        sync.RWMutex
	userLastEvent map[userGuildKey]map[EventType]time.Time

	guildMu       sync.RWMutex
	guildCounters map[uint64]map[EventType]int
	guildResetAt  time.Time

	globalMu       sync.RWMutex
	globalCounters map[EventType]int
	globalResetAt  time.Time
}

// userGuildKey uniquely identifies a user within a guild.
type userGuildKey struct {
	userID  uint64
	guildID uint64
}

// New creates a new rate limiter with the given configuration.
func New(config *Config, logger *zap.Logger) *Limiter {
	if config == nil {
		config = DefaultConfig()
	}

	return &Limiter{
		config:         config,
		logger:         logger,
		userLastEvent:  make(map[userGuildKey]map[EventType]time.Time),
		guildCounters:  make(map[uint64]map[EventType]int),
		guildResetAt:   time.Now().Add(config.PerGuildResetPeriod),
		globalCounters: make(map[EventType]int),
		globalResetAt:  time.Now().Add(config.GlobalResetPeriod),
	}
}

// Allow determines if an event should be allowed through the rate limiter.
// Returns true if the event should be processed, false if it should be dropped.
func (l *Limiter) Allow(eventType EventType, userID, guildID uint64) bool {
	now := time.Now()

	// Check user cooldown period
	if !l.allowUser(eventType, userID, guildID, now) {
		return false
	}

	// Check guild limits
	if !l.allowGuild(eventType, guildID, now) {
		return false
	}

	// Check global limits
	if !l.allowGlobal(eventType, now) {
		return false
	}

	return true
}

// allowUser checks if an event from this user in this guild is allowed
// based on per-user cooldown settings.
func (l *Limiter) allowUser(eventType EventType, userID, guildID uint64, now time.Time) bool {
	key := userGuildKey{userID: userID, guildID: guildID}
	cooldown, exists := l.config.PerUserCooldown[eventType]
	if !exists {
		// If no cooldown configured, always allow
		return true
	}

	l.userMu.Lock()
	defer l.userMu.Unlock()

	events, exists := l.userLastEvent[key]
	if !exists {
		l.userLastEvent[key] = map[EventType]time.Time{eventType: now}
		return true
	}

	lastTime, exists := events[eventType]
	if !exists || now.Sub(lastTime) >= cooldown {
		events[eventType] = now
		return true
	}

	// Still in cooldown
	return false
}

// allowGuild checks if an event from this guild is allowed
// based on per-guild rate limits.
func (l *Limiter) allowGuild(eventType EventType, guildID uint64, now time.Time) bool {
	limit, exists := l.config.PerGuildLimit[eventType]
	if !exists || limit <= 0 {
		// If no limit configured, always allow
		return true
	}

	l.guildMu.Lock()
	defer l.guildMu.Unlock()

	// Check if we need to reset the counters
	if now.After(l.guildResetAt) {
		l.guildCounters = make(map[uint64]map[EventType]int)
		l.guildResetAt = now.Add(l.config.PerGuildResetPeriod)
	}

	// Check if this guild has a counter
	counters, exists := l.guildCounters[guildID]
	if !exists {
		counters = make(map[EventType]int)
		l.guildCounters[guildID] = counters
	}

	// Check if we're under the limit
	count := counters[eventType]
	if count >= limit {
		return false
	}

	// Increment the counter
	counters[eventType] = count + 1
	return true
}

// allowGlobal checks if an event is allowed based on global rate limits.
func (l *Limiter) allowGlobal(eventType EventType, now time.Time) bool {
	limit, exists := l.config.GlobalLimit[eventType]
	if !exists || limit <= 0 {
		// If no limit configured, always allow
		return true
	}

	l.globalMu.Lock()
	defer l.globalMu.Unlock()

	// Check if we need to reset the counters
	if now.After(l.globalResetAt) {
		l.globalCounters = make(map[EventType]int)
		l.globalResetAt = now.Add(l.config.GlobalResetPeriod)
	}

	// Check if we're under the limit
	count := l.globalCounters[eventType]
	if count >= limit {
		return false
	}

	// Increment the counter
	l.globalCounters[eventType] = count + 1
	return true
}

// Cleanup removes old entries from the rate limiter to prevent memory leaks.
// This should be called periodically, e.g., every hour.
func (l *Limiter) Cleanup() {
	now := time.Now()

	// Find the maximum cooldown period
	var maxCooldown time.Duration
	for _, cooldown := range l.config.PerUserCooldown {
		if cooldown > maxCooldown {
			maxCooldown = cooldown
		}
	}

	// Remove user entries older than the maximum cooldown
	l.userMu.Lock()
	for key, events := range l.userLastEvent {
		allOld := true
		for eventType, lastTime := range events {
			cooldown := l.config.PerUserCooldown[eventType]
			if now.Sub(lastTime) < cooldown {
				allOld = false
				break
			}
		}
		if allOld {
			delete(l.userLastEvent, key)
		}
	}
	l.userMu.Unlock()
}

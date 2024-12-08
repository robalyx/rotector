package ratelimit

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/jaxron/axonet/pkg/client/logger"
	"github.com/jaxron/axonet/pkg/client/middleware"
	"github.com/redis/rueidis"
)

const (
	// KeyPrefix is the prefix for the rate limit keys in Redis.
	KeyPrefix = "ratelimit:global"

	// WaitTime is the fixed minimum wait time when rate limit is exceeded.
	WaitTime = 100 * time.Millisecond
)

// RateLimiter implements a distributed rate limiting middleware using Redis.
type RateLimiter struct {
	client            rueidis.Client
	requestsPerSecond float64
	logger            logger.Logger
}

// NewRateLimiter creates a new RateLimiter instance.
func NewRateLimiter(client rueidis.Client, requestsPerSecond float64) *RateLimiter {
	return &RateLimiter{
		client:            client,
		requestsPerSecond: requestsPerSecond,
		logger:            &logger.NoOpLogger{},
	}
}

// Process applies distributed rate limiting before passing the request to the next middleware.
func (m *RateLimiter) Process(ctx context.Context, httpClient *http.Client, req *http.Request, next middleware.NextFunc) (*http.Response, error) {
	// Keep trying until we get capacity or context is cancelled
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
			if m.tryAcquire(ctx) {
				return next(ctx, httpClient, req)
			}

			m.logger.WithFields().Debug("Rate limit exceeded, waiting...")

			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(WaitTime):
				continue
			}
		}
	}
}

// tryAcquire attempts to acquire capacity from the rate limiter.
// Returns true if successful, false if over limit.
func (m *RateLimiter) tryAcquire(ctx context.Context) bool {
	// Current timestamp in seconds
	now := time.Now().UTC().Unix()
	currentKey := fmt.Sprintf("%s:%d", KeyPrefix, now)
	lastKey := fmt.Sprintf("%s:%d", KeyPrefix, now-1)

	// Use Lua script for atomic increment and check
	script := `
		local current = tonumber(redis.call('GET', KEYS[1]) or 0)
		local last = tonumber(redis.call('GET', KEYS[2]) or 0)
		local weight = tonumber(ARGV[1])
		local limit = tonumber(ARGV[2])
		
		-- Calculate weighted count for sliding window
		local weighted_count = (last * weight) + current
		
		-- If we're at or over the limit, return current state
		if weighted_count >= limit then
			-- Clean up old window if it exists
			if last > 0 then
				redis.call('DEL', KEYS[2])
			end
			return 0
		end
		
		-- Increment current window
		redis.call('INCR', KEYS[1])
		redis.call('EXPIRE', KEYS[1], 2)
		
		-- Clean up old window if it exists
		if last > 0 then
			redis.call('DEL', KEYS[2])
		end
		
		return 1
	`

	// Calculate the weight for the sliding window
	subSecond := float64(time.Now().UTC().Nanosecond()) / float64(time.Second)
	weight := 1.0 - subSecond

	// Execute the Lua script
	resp := m.client.Do(ctx, m.client.B().Eval().
		Script(script).
		Numkeys(2).
		Key(currentKey).
		Key(lastKey).
		Arg(fmt.Sprintf("%.6f", weight)).
		Arg(fmt.Sprintf("%.6f", m.requestsPerSecond)).
		Build())

	if resp.Error() != nil {
		// On error, deny the request to be safe
		return false
	}

	// Parse the response
	allowed, err := resp.AsInt64()
	if err != nil {
		return false
	}

	return allowed == 1
}

// SetLogger sets the logger for the middleware.
func (m *RateLimiter) SetLogger(l logger.Logger) {
	m.logger = l
}

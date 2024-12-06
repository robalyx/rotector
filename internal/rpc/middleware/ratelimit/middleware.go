package ratelimit

import (
	"context"
	"sync"

	"github.com/rotector/rotector/internal/common/config"
	"github.com/rotector/rotector/internal/rpc/middleware/ip"
	"github.com/twitchtv/twirp"
	"golang.org/x/time/rate"
)

// Middleware implements rate limiting middleware for Twirp services.
type Middleware struct {
	limiters     map[string]*rate.Limiter
	limiterMutex sync.RWMutex
	config       *config.RPCRateLimit
}

// New creates a new rate limiting middleware.
func New(config *config.RPCRateLimit) *Middleware {
	return &Middleware{
		limiters: make(map[string]*rate.Limiter),
		config:   config,
	}
}

// ServerHooks returns Twirp server hooks for rate limiting.
func (m *Middleware) ServerHooks() *twirp.ServerHooks {
	return &twirp.ServerHooks{
		RequestReceived: m.requestReceived,
	}
}

// requestReceived handles incoming requests and checks the rate limit for the client IP.
func (m *Middleware) requestReceived(ctx context.Context) (context.Context, error) {
	// Get client IP from request
	clientIP := ip.FromContext(ctx)

	// Check rate limit for this IP
	limiter := m.getLimiter(clientIP)
	if !limiter.Allow() {
		return ctx, twirp.NewError(twirp.ResourceExhausted, "rate limit exceeded")
	}

	return ctx, nil
}

// getLimiter retrieves or creates a rate limiter for the given IP.
func (m *Middleware) getLimiter(ip string) *rate.Limiter {
	// Try to get existing limiter
	m.limiterMutex.RLock()
	limiter, exists := m.limiters[ip]
	m.limiterMutex.RUnlock()

	if !exists {
		// Create new limiter if none exists
		m.limiterMutex.Lock()
		limiter, exists = m.limiters[ip]
		if !exists {
			limiter = rate.NewLimiter(rate.Limit(m.config.RequestsPerSecond), m.config.BurstSize)
			m.limiters[ip] = limiter
		}
		m.limiterMutex.Unlock()
	}

	return limiter
}

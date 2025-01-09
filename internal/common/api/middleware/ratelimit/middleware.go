package ratelimit

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/robalyx/rotector/internal/common/api/middleware/ip"
	"github.com/robalyx/rotector/internal/common/setup/config"
	"github.com/robalyx/rotector/internal/common/storage/database"
	"github.com/robalyx/rotector/internal/common/utils"
	"github.com/twitchtv/twirp"
	"github.com/uptrace/bunrouter"
	"go.uber.org/zap"
	"golang.org/x/time/rate"
)

const (
	errBlocked    = "temporarily blocked for repeated rate limit violations"
	errRateLimit  = "rate limit exceeded"
	headerRetryAt = "Retry-After"
)

type limiterState struct {
	limiter      *rate.Limiter
	strikes      int       // Number of times client has violated rate limit
	blockedUntil time.Time // Time until client is blocked for repeated violations
	isAPIKey     bool      // Whether this is an API key client
}

// Middleware implements rate limiting for API requests.
type Middleware struct {
	limiters *utils.TTLMap[string, *limiterState]
	config   *config.RateLimit
	db       *database.Client
	logger   *zap.Logger
}

// New creates a new rate limiting middleware.
func New(config *config.RateLimit, db *database.Client, logger *zap.Logger) *Middleware {
	// Use the longer of block duration or burst window * 2 for TTL
	ttl := time.Second * time.Duration(config.BurstSize*2)
	if blockTTL := time.Second * time.Duration(config.BlockDuration*2); blockTTL > ttl {
		ttl = blockTTL
	}

	return &Middleware{
		limiters: utils.NewTTLMap[string, *limiterState](ttl),
		config:   config,
		db:       db,
		logger:   logger,
	}
}

// AsRPCHooks returns Twirp server hooks for rate limiting in RPC server.
func (m *Middleware) AsRPCHooks() *twirp.ServerHooks {
	return &twirp.ServerHooks{
		RequestReceived: func(ctx context.Context) (context.Context, error) {
			clientIP := ip.FromContext(ctx)
			if allowed, retryAfter, err := m.checkRateLimit(ctx, clientIP); !allowed {
				// Add retry header to context for Twirp to include in response
				if retryAfter > 0 {
					if headerErr := twirp.AddHTTPResponseHeader(ctx, headerRetryAt,
						fmt.Sprintf("%.0f", retryAfter.Seconds())); headerErr != nil {
						m.logger.Error("Failed to add retry header", zap.Error(headerErr))
					}
				}
				return ctx, err
			}
			return ctx, nil
		},
	}
}

// AsRESTMiddleware returns a bunrouter middleware handler for rate limiting in REST server.
func (m *Middleware) AsRESTMiddleware(next bunrouter.HandlerFunc) bunrouter.HandlerFunc {
	return func(w http.ResponseWriter, req bunrouter.Request) error {
		clientIP := ip.FromContext(req.Context())
		if allowed, retryAfter, err := m.checkRateLimit(req.Context(), clientIP); !allowed {
			// Add Retry-After header if there's a wait time
			if retryAfter > 0 {
				w.Header().Set(headerRetryAt, fmt.Sprintf("%.0f", retryAfter.Seconds()))
			}

			var twerr twirp.Error
			if errors.As(err, &twerr) {
				http.Error(w, twerr.Msg(), http.StatusTooManyRequests)
			} else {
				http.Error(w, errRateLimit, http.StatusTooManyRequests)
			}
			return nil
		}
		return next(w, req)
	}
}

// getLimiter returns a rate limiter for the specified IP.
func (m *Middleware) getLimiter(ctx context.Context, clientIP string) *limiterState {
	// Try to get existing limiter
	if state, exists := m.limiters.Get(clientIP); exists {
		return state
	}

	// Check if request has valid API key
	isAPIKey := false
	if apiKey := m.getAPIKey(ctx, clientIP); apiKey != "" {
		isAPIKey = true
	}

	// Create new limiter with appropriate limits
	var limiter *rate.Limiter
	if isAPIKey {
		limiter = rate.NewLimiter(rate.Limit(m.config.APIKeyRequestsPerSec), m.config.APIKeyBurstSize)
	} else {
		limiter = rate.NewLimiter(rate.Limit(m.config.RequestsPerSecond), m.config.BurstSize)
	}

	state := &limiterState{
		limiter:  limiter,
		isAPIKey: isAPIKey,
	}
	m.limiters.Set(clientIP, state)
	return state
}

// handleStrikes checks if strikes exceed limit and blocks if necessary.
func (m *Middleware) handleStrikes(state *limiterState, clientIP string) (bool, time.Duration, error) {
	if state.strikes >= m.config.StrikeLimit {
		blockDuration := time.Duration(m.config.BlockDuration) * time.Second
		state.blockedUntil = time.Now().Add(blockDuration)
		state.strikes = 0 // Reset strikes

		m.logger.Debug("Client exceeded strike limit and is now blocked",
			zap.String("ip", clientIP),
			zap.Int("strikes", m.config.StrikeLimit),
			zap.Duration("block_duration", blockDuration))

		return false, blockDuration, twirp.NewError(twirp.ResourceExhausted, errBlocked)
	}
	return true, 0, nil
}

// checkBlocked checks if the client is currently blocked.
func (m *Middleware) checkBlocked(state *limiterState, clientIP string) (bool, time.Duration, error) {
	if !state.blockedUntil.IsZero() && time.Now().Before(state.blockedUntil) {
		retryAfter := time.Until(state.blockedUntil).Round(time.Second)
		m.logger.Debug("Client is temporarily blocked",
			zap.String("ip", clientIP),
			zap.Duration("retry_after", retryAfter))
		return false, retryAfter, twirp.NewError(twirp.ResourceExhausted, errBlocked)
	}
	return true, 0, nil
}

// checkRateLimit checks if the request should be allowed and updates violation tracking.
func (m *Middleware) checkRateLimit(ctx context.Context, clientIP string) (bool, time.Duration, error) {
	state := m.getLimiter(ctx, clientIP)

	// Check if client is blocked
	if allowed, retryAfter, err := m.checkBlocked(state, clientIP); !allowed {
		return allowed, retryAfter, err
	}

	// Try to reserve a token
	reservation := state.limiter.Reserve()
	if !reservation.OK() {
		state.strikes++

		// Check if we should block the client
		if allowed, retryAfter, err := m.handleStrikes(state, clientIP); !allowed {
			return allowed, retryAfter, err
		}

		m.logger.Debug("Rate limit exceeded",
			zap.String("ip", clientIP),
			zap.Int("strikes", state.strikes))

		return false, 0, twirp.NewError(twirp.ResourceExhausted, errRateLimit)
	}

	// Get delay for this reservation
	delay := reservation.Delay()
	if delay > 0 {
		state.strikes++
		reservation.Cancel()

		// Check if we should block the client
		if allowed, retryAfter, err := m.handleStrikes(state, clientIP); !allowed {
			return allowed, retryAfter, err
		}

		m.logger.Debug("Rate limit delay required",
			zap.String("ip", clientIP),
			zap.Duration("delay", delay),
			zap.Int("strikes", state.strikes))
		return false, delay, twirp.NewError(twirp.ResourceExhausted, errRateLimit)
	}

	// Reset strikes on successful request
	if state.strikes > 0 {
		state.strikes = 0
	}

	return true, 0, nil
}

// getAPIKey checks if the request has a valid API key.
func (m *Middleware) getAPIKey(ctx context.Context, clientIP string) string {
	var authHeader string

	// Try to get headers from Twirp context first
	if header, ok := twirp.HTTPRequestHeaders(ctx); ok {
		authHeader = header.Get("Authorization")
	}

	// No Authorization header found
	if authHeader == "" {
		return ""
	}

	// Remove "Bearer " prefix if present
	apiKey := strings.TrimPrefix(authHeader, "Bearer ")

	// Get bot settings from database
	botSettings, err := m.db.Settings().GetBotSettings(ctx)
	if err != nil {
		m.logger.Error("Failed to get bot settings", zap.Error(err))
		return ""
	}

	// Check if API key is valid
	if info, exists := botSettings.IsAPIKey(apiKey); exists {
		m.logger.Debug("Valid API key found",
			zap.String("ip", clientIP),
			zap.String("description", info.Description))
		return apiKey
	}

	return ""
}

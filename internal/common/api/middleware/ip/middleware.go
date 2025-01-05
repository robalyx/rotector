package ip

import (
	"context"
	"net"
	"net/http"
	"strings"

	"github.com/rotector/rotector/internal/common/api/middleware/header"
	"github.com/rotector/rotector/internal/common/setup/config"
	"github.com/twitchtv/twirp"
	"github.com/uptrace/bunrouter"
	"go.uber.org/zap"
)

type (
	ipCtxKey struct{}
)

// UnknownIP is returned when no valid IP can be determined.
const UnknownIP = "unknown"

// FromContext retrieves the client IP from the context.
func FromContext(ctx context.Context) string {
	if ip, ok := ctx.Value(ipCtxKey{}).(string); ok {
		return ip
	}
	return UnknownIP
}

// Middleware handles IP detection and stores it in the context.
type Middleware struct {
	checker *Checker
	logger  *zap.Logger
	config  *config.IPConfig
}

// New creates a new IP middleware.
func New(logger *zap.Logger, config *config.IPConfig) *Middleware {
	return &Middleware{
		checker: NewChecker(logger, config),
		logger:  logger,
		config:  config,
	}
}

// AsRPCHooks returns Twirp server hooks for IP validation in RPC server.
func (m *Middleware) AsRPCHooks() *twirp.ServerHooks {
	return &twirp.ServerHooks{
		RequestReceived: func(ctx context.Context) (context.Context, error) {
			ip := m.getClientIP(ctx)
			if ip == UnknownIP {
				m.logger.Warn("No valid client IP found in request")
				return ctx, twirp.NewError(twirp.PermissionDenied, "request must include a valid public IP address")
			}

			m.logger.Debug("Client IP detected", zap.String("ip", ip))
			return context.WithValue(ctx, ipCtxKey{}, ip), nil
		},
	}
}

// AsRESTMiddleware returns a bunrouter middleware handler for IP validation in REST server.
func (m *Middleware) AsRESTMiddleware(next bunrouter.HandlerFunc) bunrouter.HandlerFunc {
	return func(w http.ResponseWriter, req bunrouter.Request) error {
		// Get client IP from context
		ip := m.getClientIP(req.Context())
		if ip == UnknownIP {
			http.Error(w, "Invalid IP address", http.StatusForbidden)
			return nil
		}

		// Store IP in context for handlers
		ctx := context.WithValue(req.Context(), ipCtxKey{}, ip)
		req = req.WithContext(ctx)

		return next(w, req)
	}
}

// getClientIP extracts the client IP from the request context.
func (m *Middleware) getClientIP(ctx context.Context) string {
	// Get headers from context
	headers, ok := twirp.HTTPRequestHeaders(ctx)
	if !ok {
		m.logger.Debug("No headers found in context")
		return UnknownIP
	}

	// Get and validate remote address
	remoteIP := m.getRemoteIP(ctx)
	if remoteIP == nil {
		m.logger.Debug("Failed to get valid remote IP")
		return UnknownIP
	}
	m.logger.Debug("Got remote IP", zap.String("ip", remoteIP.String()))

	// If header checking is disabled, use remote address directly
	if !m.config.EnableHeaderCheck {
		return m.useRemoteIP(remoteIP, "Header checking is disabled")
	}

	// If remote IP is a trusted proxy, check headers
	if m.checker.IsTrustedProxy(remoteIP) {
		if ip := m.getIPFromHeaders(headers); ip != UnknownIP {
			m.logger.Debug("Found valid IP in headers", zap.String("ip", ip))
			return ip
		}
		m.logger.Debug("No valid IP found in headers")
	}

	// If not a trusted proxy or no valid headers found, validate remote IP
	return m.useRemoteIP(remoteIP, "Using remote IP")
}

// useRemoteIP validates and returns the remote IP with appropriate logging.
func (m *Middleware) useRemoteIP(remoteIP net.IP, reason string) string {
	if m.checker.IsValidPublicIP(remoteIP) {
		m.logger.Debug(reason, zap.String("ip", remoteIP.String()))
		return remoteIP.String()
	}
	m.logger.Debug("Remote IP is not a valid public IP")
	return UnknownIP
}

// getRemoteIP gets and validates the remote IP from the context.
func (m *Middleware) getRemoteIP(ctx context.Context) net.IP {
	// Get remote address from context
	remoteAddr := header.FromRemoteAddr(ctx)
	if remoteAddr == "" {
		m.logger.Debug("No remote address in context")
		return nil
	}

	// Parse remote address
	remoteIP, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		m.logger.Debug("Failed to parse remote address",
			zap.String("addr", remoteAddr),
			zap.Error(err))
		return nil
	}

	parsedIP := net.ParseIP(remoteIP)
	if parsedIP == nil {
		m.logger.Debug("Invalid remote IP", zap.String("ip", remoteIP))
		return nil
	}

	return parsedIP
}

// getIPFromHeaders attempts to get a valid IP from the configured headers.
func (m *Middleware) getIPFromHeaders(header http.Header) string {
	for _, h := range m.config.CustomHeaders {
		ip := header.Get(h)
		if ip == "" {
			continue
		}

		// Handle forwarded headers differently
		if strings.Contains(h, "Forward") {
			if validated := m.getForwardedIP(ip); validated != UnknownIP {
				return validated
			}
		} else {
			if validated := m.checker.ValidateIP(ip); validated != UnknownIP {
				return validated
			}
		}

		m.logger.Debug("IP validation failed",
			zap.String("header", h),
			zap.String("ip", ip))
	}
	return UnknownIP
}

// getForwardedIP handles the special case of forwarded headers with multiple IPs.
func (m *Middleware) getForwardedIP(forwarded string) string {
	ips := strings.Split(forwarded, ",")
	// Check IPs from right to left (closest to server first)
	for i := len(ips) - 1; i >= 0; i-- {
		if validated := m.checker.ValidateIP(strings.TrimSpace(ips[i])); validated != UnknownIP {
			return validated
		}
	}
	return UnknownIP
}

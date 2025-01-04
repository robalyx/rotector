package header

import (
	"context"
	"net/http"

	"github.com/twitchtv/twirp"
	"github.com/uptrace/bunrouter"
	"go.uber.org/zap"
)

type remoteAddrCtxKey struct{}

// FromRemoteAddr retrieves the remote address from context.
func FromRemoteAddr(ctx context.Context) string {
	if addr, ok := ctx.Value(remoteAddrCtxKey{}).(string); ok {
		return addr
	}
	return ""
}

// Middleware handles header extraction and storage.
type Middleware struct {
	logger *zap.Logger
}

// New creates a new header middleware.
func New(logger *zap.Logger) *Middleware {
	return &Middleware{
		logger: logger,
	}
}

// AsHTTPMiddleware returns an http.Handler middleware for header extraction.
func (m *Middleware) AsHTTPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, err := m.storeHeadersInContext(r.Context(), r.RemoteAddr, r.Header)
		if err != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// AsRESTMiddleware returns a bunrouter middleware handler for header extraction in REST server.
func (m *Middleware) AsRESTMiddleware(next bunrouter.HandlerFunc) bunrouter.HandlerFunc {
	return func(w http.ResponseWriter, req bunrouter.Request) error {
		ctx, err := m.storeHeadersInContext(req.Context(), req.RemoteAddr, req.Header)
		if err != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return err
		}

		return next(w, req.WithContext(ctx))
	}
}

// storeHeadersInContext stores remote address and filtered headers in context.
func (m *Middleware) storeHeadersInContext(ctx context.Context, remoteAddr string, headers http.Header) (context.Context, error) {
	// Store remote address
	ctx = context.WithValue(ctx, remoteAddrCtxKey{}, remoteAddr)
	m.logger.Debug("Stored remote address",
		zap.String("addr", remoteAddr))

	// Create filtered headers for Twirp
	filtered := make(http.Header)
	for k, v := range headers {
		switch k {
		case "Accept", "Content-Type", "Twirp-Version":
			// Exclude headers that Twirp manages internally
			continue
		default:
			filtered[k] = v
		}
	}

	// Store filtered headers in Twirp context
	var err error
	ctx, err = twirp.WithHTTPRequestHeaders(ctx, filtered)
	if err != nil {
		m.logger.Error("Failed to set headers in context", zap.Error(err))
		return nil, err
	}

	return ctx, nil
}

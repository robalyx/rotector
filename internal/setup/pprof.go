package setup

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	// #nosec G108 -- pprof debugging is intentionally enabled only on localhost
	_ "net/http/pprof"
	"time"

	"go.uber.org/zap"
)

// pprofServer represents the pprof HTTP server.
type pprofServer struct {
	srv      *http.Server
	listener net.Listener
}

// startPprofServer initializes and starts the pprof HTTP server.
func startPprofServer(port int, logger *zap.Logger) (*pprofServer, error) {
	pprofAddr := fmt.Sprintf("localhost:%d", port)

	// Create secure server with timeouts
	srv := &http.Server{
		Addr:              pprofAddr,
		Handler:           http.DefaultServeMux,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Only listen on localhost
	listener, err := net.Listen("tcp", pprofAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to create listener: %w", err)
	}

	// Start server in background
	go func() {
		logger.Info("Starting pprof server", zap.String("address", pprofAddr))
		if err := srv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("Pprof server failed", zap.Error(err))
		}
	}()

	return &pprofServer{
		srv:      srv,
		listener: listener,
	}, nil
}

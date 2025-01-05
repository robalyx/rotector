package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rotector/rotector/internal/common/setup"
	"github.com/rotector/rotector/internal/rpc"
	"go.uber.org/zap"
)

// RPCLogDir specifies where RPC server log files are stored.
const RPCLogDir = "logs/rpc_logs"

// Server timeouts.
const (
	ReadTimeout     = 5 * time.Second
	WriteTimeout    = 10 * time.Second
	ShutdownTimeout = 30 * time.Second
)

func main() {
	// Initialize application with required dependencies
	app, err := setup.InitializeApp(context.Background(), RPCLogDir)
	if err != nil {
		log.Fatalf("Failed to initialize application: %v", err)
	}
	defer app.Cleanup(context.Background())

	// Create server
	handler, err := rpc.NewServer(app.DB, app.Logger, &app.Config.API)
	if err != nil {
		app.Logger.Fatal("Failed to create RPC server", zap.Error(err))
	}

	// Create HTTP server multiplexer
	mux := http.NewServeMux()
	mux.Handle("/twirp/", handler)

	// Get server address from config
	addr := fmt.Sprintf("%s:%d", app.Config.API.Server.Host, app.Config.API.Server.Port)

	// Create HTTP server with timeouts
	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  ReadTimeout,
		WriteTimeout: WriteTimeout,
	}

	// Start server in a goroutine
	go func() {
		log.Printf("RPC server started on %s", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			app.Logger.Error("Failed to start server", zap.Error(err))
		}
	}()

	// Wait for interrupt signal
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	app.Logger.Info("Shutting down RPC server...")

	// Create shutdown context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), ShutdownTimeout)
	defer cancel()

	// Attempt graceful shutdown
	if err := srv.Shutdown(ctx); err != nil {
		app.Logger.Error("Server forced to shutdown", zap.Error(err))
	}

	app.Logger.Info("Server gracefully stopped")
}

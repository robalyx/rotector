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

	_ "github.com/robalyx/rotector/docs/api"
	"github.com/robalyx/rotector/internal/common/setup"
	"github.com/robalyx/rotector/internal/rest"
	"go.uber.org/zap"
)

// RESTLogDir specifies where REST server log files are stored.
const RESTLogDir = "logs/rest_logs"

// Server timeouts.
const (
	ReadTimeout     = 5 * time.Second
	WriteTimeout    = 10 * time.Second
	ShutdownTimeout = 30 * time.Second
)

//	@title			Rotector API
//	@version		1.0
//	@description	REST API for Rotector

//	@license.name	GPL-2.0
//	@license.url	https://www.gnu.org/licenses/old-licenses/gpl-2.0.en.html

//	@BasePath	/v1

// @securityDefinitions.apikey	BearerAuth
// @in							header
// @name						Authorization
// @description				API key must be provided as: Bearer <api_key>
func main() {
	// Initialize application with required dependencies
	app, err := setup.InitializeApp(context.Background(), setup.ServiceREST, RESTLogDir)
	if err != nil {
		log.Fatalf("Failed to initialize application: %v", err)
	}
	defer app.Cleanup(context.Background())

	// Create server
	handler, err := rest.NewServer(app.DB, app.Logger, &app.Config.API)
	if err != nil {
		app.Logger.Fatal("Failed to create REST server", zap.Error(err))
	}

	// Get server address from config
	addr := fmt.Sprintf("%s:%d", app.Config.API.Server.Host, app.Config.API.Server.Port)

	// Create HTTP server with timeouts
	srv := &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  ReadTimeout,
		WriteTimeout: WriteTimeout,
	}

	// Start server in a goroutine
	go func() {
		log.Printf("REST server started on %s", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			app.Logger.Error("Failed to start server", zap.Error(err))
		}
	}()

	// Wait for interrupt signal
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	app.Logger.Info("Shutting down REST server...")

	// Create shutdown context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), ShutdownTimeout)
	defer cancel()

	// Attempt graceful shutdown
	if err := srv.Shutdown(ctx); err != nil {
		app.Logger.Error("Server forced to shutdown", zap.Error(err))
	}

	app.Logger.Info("Server gracefully stopped")
}

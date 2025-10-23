package setup

import (
	"context"
	"fmt"
	"log"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/redis/rueidis"
	aiClient "github.com/robalyx/rotector/internal/ai/client"
	"github.com/robalyx/rotector/internal/cloudflare"
	"github.com/robalyx/rotector/internal/database"
	"github.com/robalyx/rotector/internal/database/migrations"
	"github.com/robalyx/rotector/internal/redis"
	"github.com/robalyx/rotector/internal/setup/client"
	"github.com/robalyx/rotector/internal/setup/config"
	"github.com/robalyx/rotector/internal/setup/telemetry"
	"github.com/uptrace/bun/migrate"
	"go.uber.org/zap"
)

// App bundles all core dependencies and services needed by the application.
// Each field represents a major subsystem that needs initialization and cleanup.
type App struct {
	Config       *config.Config      // Application configuration
	Logger       *zap.Logger         // Main application logger
	DBLogger     *zap.Logger         // Database-specific logger
	DB           database.Client     // Database connection pool
	AIClient     *aiClient.AIClient  // AI client providers
	RoAPI        *api.API            // RoAPI HTTP client
	RedisManager *redis.Manager      // Redis connection manager
	StatusClient rueidis.Client      // Redis client for worker status reporting
	CFClient     *cloudflare.Client  // Cloudflare D1 client for cloudflare operations
	LogManager   *telemetry.Manager  // Log management system
	pprofServer  *pprofServer        // Debug HTTP server for pprof
	middlewares  *client.Middlewares // HTTP client middleware instances
}

// InitializeApp bootstraps all application dependencies in the correct order,
// ensuring each component has its required dependencies available.
// Workers can provide type and ID information for service identification.
func InitializeApp(ctx context.Context, serviceType telemetry.ServiceType, logDir string, workerInfo ...string) (*App, error) {
	// Load app configuration
	cfg, configDir, err := config.LoadConfig()
	if err != nil {
		return nil, err
	}

	// Extract worker information if provided
	var workerType, workerID string
	if len(workerInfo) >= 2 {
		workerType = workerInfo[0]
		workerID = workerInfo[1]
	}

	// Logging system is initialized next to capture setup issues
	logManager := telemetry.NewManager(
		ctx, serviceType, logDir, &cfg.Common.Debug, &cfg.Common.Loki, workerType, workerID,
	)

	logger, dbLogger, err := logManager.GetLoggers()
	if err != nil {
		return nil, err
	}

	// Redis manager provides connection pools for various subsystems
	redisManager := redis.NewManager(&cfg.Common.Redis, logger)

	// Initialize database with migration check
	db, err := checkAndRunMigrations(ctx, &cfg.Common.PostgreSQL, dbLogger)
	if err != nil {
		return nil, err
	}

	// RoAPI client is configured with middleware chain
	requestTimeout := serviceType.GetRequestTimeout(cfg)

	roAPI, middlewares, err := client.GetRoAPIClient(ctx, &cfg.Common, configDir, redisManager, logger, requestTimeout)
	if err != nil {
		return nil, err
	}

	// Log information about proxy configuration
	if len(middlewares.Proxy.GetProxies()) > 0 {
		logger.Info("Initialized regular proxies", zap.Int("count", len(middlewares.Proxy.GetProxies())))
	}

	if len(middlewares.Roverse.GetProxies()) > 0 {
		logger.Info("Initialized roverse proxies", zap.Int("count", len(middlewares.Roverse.GetProxies())))
	}

	// Get Redis client for worker status reporting
	statusClient, err := redisManager.GetClient(redis.WorkerStatusDBIndex)
	if err != nil {
		return nil, err
	}

	// Initialize D1 client for cloudflare operations
	cfClient := cloudflare.NewClient(cfg, db, logger)

	// Initialize AI client
	aiCli, err := aiClient.NewClient(&cfg.Common.OpenAI, cfClient.AIUsage, logger)
	if err != nil {
		return nil, err
	}

	// Start pprof server if enabled
	var pprofSrv *pprofServer

	if cfg.Common.Debug.EnablePprof {
		srv, err := startPprofServer(ctx, cfg.Common.Debug.PprofPort, logger)
		if err != nil {
			logger.Error("Failed to start pprof server", zap.Error(err))
		} else {
			pprofSrv = srv

			logger.Warn("pprof debugging endpoint enabled - this should not be used in production!")
		}
	}

	// Bundle all initialized components
	return &App{
		Config:       cfg,
		Logger:       logger,
		DBLogger:     dbLogger.Named("database"),
		DB:           db,
		AIClient:     aiCli,
		RoAPI:        roAPI,
		RedisManager: redisManager,
		StatusClient: statusClient,
		CFClient:     cfClient,
		LogManager:   logManager,
		pprofServer:  pprofSrv,
		middlewares:  middlewares,
	}, nil
}

// Cleanup ensures graceful shutdown of all components in reverse initialization order.
// Logs but does not fail on cleanup errors to ensure all components get cleanup attempts.
func (s *App) Cleanup(ctx context.Context) {
	// Shutdown pprof server if running
	if s.pprofServer != nil {
		if err := s.pprofServer.srv.Shutdown(ctx); err != nil {
			s.Logger.Error("Failed to shutdown pprof server", zap.Error(err))
		}

		s.pprofServer.listener.Close()
	}

	// Sync buffered logs before shutdown
	if err := s.Logger.Sync(); err != nil {
		log.Printf("Failed to sync logger: %v", err)
	}

	if err := s.DBLogger.Sync(); err != nil {
		log.Printf("Failed to sync DB logger: %v", err)
	}

	// Stop telemetry manager to flush Loki logs
	s.LogManager.Stop()

	// Close database connections
	if err := s.DB.Close(); err != nil {
		log.Printf("Failed to close database connection: %v", err)
	}

	// Cleanup proxy and roverse middlewares
	s.middlewares.Proxy.Cleanup()
	s.middlewares.Roverse.Cleanup()

	// Close Redis connections last as other components might need it during cleanup
	s.RedisManager.Close()
}

// checkAndRunMigrations runs database migrations if needed.
func checkAndRunMigrations(ctx context.Context, cfg *config.PostgreSQL, dbLogger *zap.Logger) (database.Client, error) {
	tempDB, err := database.NewConnection(ctx, cfg, dbLogger, false)
	if err != nil {
		return nil, err
	}

	migrator := migrate.NewMigrator(tempDB.DB(), migrations.Migrations)

	ms, err := migrator.MigrationsWithStatus(ctx)
	if err != nil {
		tempDB.Close()
		return nil, fmt.Errorf("failed to check migration status: %w", err)
	}

	var db database.Client

	unapplied := ms.Unapplied()
	if len(unapplied) > 0 {
		log.Println("Database migrations are pending. Would you like to run them now? (y/N)")

		var response string

		_, _ = fmt.Scanln(&response)

		if response == "y" || response == "Y" {
			tempDB.Close()

			db, err = database.NewConnection(ctx, cfg, dbLogger, true)
		} else {
			log.Fatalf("Closing program due to incomplete migrations")
		}
	} else {
		db = tempDB
	}

	return db, err
}

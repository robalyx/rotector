package setup

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/redis/rueidis"
	"github.com/robalyx/rotector/internal/common/queue"
	"github.com/robalyx/rotector/internal/common/setup/client"
	"github.com/robalyx/rotector/internal/common/setup/config"
	"github.com/robalyx/rotector/internal/common/setup/logger"
	"github.com/robalyx/rotector/internal/common/storage/database"
	"github.com/robalyx/rotector/internal/common/storage/database/migrations"
	"github.com/robalyx/rotector/internal/common/storage/redis"
	"github.com/uptrace/bun/migrate"
	"github.com/uptrace/uptrace-go/uptrace"
	"go.uber.org/zap"
)

// ServiceType identifies which service is being initialized.
type ServiceType int

const (
	ServiceBot ServiceType = iota
	ServiceWorker
	ServiceExport
)

// GetRequestTimeout returns the request timeout for the given service type.
func (s ServiceType) GetRequestTimeout(cfg *config.Config) time.Duration {
	var timeout int
	switch s {
	case ServiceWorker:
		timeout = cfg.Worker.RequestTimeout
	case ServiceBot:
		timeout = cfg.Bot.RequestTimeout
	case ServiceExport:
		timeout = 30000
	default:
		timeout = 5000
	}

	return time.Duration(timeout) * time.Millisecond
}

// App bundles all core dependencies and services needed by the application.
// Each field represents a major subsystem that needs initialization and cleanup.
type App struct {
	Config       *config.Config      // Application configuration
	Logger       *zap.Logger         // Main application logger
	DBLogger     *zap.Logger         // Database-specific logger
	DB           database.Client     // Database connection pool
	OpenAIClient *openai.Client      // OpenAI client
	GenAIModel   string              // Generative AI model
	RoAPI        *api.API            // RoAPI HTTP client
	Queue        *queue.Manager      // Background job queue
	RedisManager *redis.Manager      // Redis connection manager
	StatusClient rueidis.Client      // Redis client for worker status reporting
	LogManager   *logger.Manager     // Log management system
	pprofServer  *pprofServer        // Debug HTTP server for pprof
	middlewares  *client.Middlewares // HTTP client middleware instances
}

// InitializeApp bootstraps all application dependencies in the correct order,
// ensuring each component has its required dependencies available.
func InitializeApp(ctx context.Context, serviceType ServiceType, logDir string) (*App, error) {
	// Configuration must be loaded first as other components depend on it
	cfg, configDir, err := config.LoadConfig()
	if err != nil {
		return nil, err
	}

	// Configure OpenTelemetry with Uptrace if enabled
	if cfg.Common.Uptrace.DSN != "" {
		uptrace.ConfigureOpentelemetry(
			uptrace.WithDSN(cfg.Common.Uptrace.DSN),
			uptrace.WithServiceName(cfg.Common.Uptrace.ServiceName),
			uptrace.WithServiceVersion(cfg.Common.Uptrace.ServiceVersion),
			uptrace.WithDeploymentEnvironment(cfg.Common.Uptrace.DeployEnvironment),
		)
	}

	// Logging system is initialized next to capture setup issues
	logManager := logger.NewManager(logDir, &cfg.Common.Debug)
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

	// Initialize OpenAI client
	openAIClient := openai.NewClient(
		option.WithAPIKey(cfg.Common.OpenAI.APIKey),
		option.WithBaseURL(cfg.Common.OpenAI.BaseURL),
		option.WithRequestTimeout(30*time.Second),
	)

	// RoAPI client is configured with middleware chain
	requestTimeout := serviceType.GetRequestTimeout(cfg)
	roAPI, middlewares, err := client.GetRoAPIClient(&cfg.Common, configDir, redisManager, logger, requestTimeout)
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

	// Queue manager creates its own Redis database for job storage
	queueClient, err := redisManager.GetClient(redis.QueueDBIndex)
	if err != nil {
		return nil, err
	}
	queueManager := queue.NewManager(queueClient, logger)

	// Get Redis client for worker status reporting
	statusClient, err := redisManager.GetClient(redis.WorkerStatusDBIndex)
	if err != nil {
		return nil, err
	}

	// Start pprof server if enabled
	var pprofSrv *pprofServer
	if cfg.Common.Debug.EnablePprof {
		srv, err := startPprofServer(cfg.Common.Debug.PprofPort, logger)
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
		OpenAIClient: &openAIClient,
		GenAIModel:   cfg.Common.OpenAI.Model,
		RoAPI:        roAPI,
		Queue:        queueManager,
		RedisManager: redisManager,
		StatusClient: statusClient,
		LogManager:   logManager,
		pprofServer:  pprofSrv,
		middlewares:  middlewares,
	}, nil
}

// Cleanup ensures graceful shutdown of all components in reverse initialization order.
// Logs but does not fail on cleanup errors to ensure all components get cleanup attempts.
func (s *App) Cleanup(ctx context.Context) {
	// Shutdown OpenTelemetry to ensure all telemetry is flushed
	if s.Config.Common.Uptrace.DSN != "" {
		if err := uptrace.Shutdown(ctx); err != nil {
			s.Logger.Error("Failed to shutdown OpenTelemetry", zap.Error(err))
		}
	}

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

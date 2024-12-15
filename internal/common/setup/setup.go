package setup

import (
	"context"
	"fmt"
	"log"
	"runtime"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/google/generative-ai-go/genai"
	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/redis/rueidis"
	"github.com/rotector/rotector/internal/common/config"
	"github.com/rotector/rotector/internal/common/queue"
	"github.com/rotector/rotector/internal/common/setup/client"
	"github.com/rotector/rotector/internal/common/setup/logger"
	"github.com/rotector/rotector/internal/common/storage/database"
	"github.com/rotector/rotector/internal/common/storage/redis"
	"go.uber.org/zap"
	"google.golang.org/api/option"
)

// App bundles all core dependencies and services needed by the application.
// Each field represents a major subsystem that needs initialization and cleanup.
type App struct {
	Config       *config.Config   // Application configuration
	Logger       *zap.Logger      // Main application logger
	DBLogger     *zap.Logger      // Database-specific logger
	DB           *database.Client // Database connection pool
	GenAIClient  *genai.Client    // Generative AI client
	GenAIModel   string           // Generative AI model
	RoAPI        *api.API         // RoAPI HTTP client
	Queue        *queue.Manager   // Background job queue
	RedisManager *redis.Manager   // Redis connection manager
	StatusClient rueidis.Client   // Redis client for worker status reporting
	LogManager   *logger.Manager  // Log management system
	pprofServer  *pprofServer     // Debug HTTP server for pprof
}

// InitializeApp bootstraps all application dependencies in the correct order,
// ensuring each component has its required dependencies available.
func InitializeApp(logDir string) (*App, error) {
	// Configuration must be loaded first as other components depend on it
	cfg, err := config.LoadConfig()
	if err != nil {
		return nil, err
	}

	// Initialize Sentry if DSN is provided
	if cfg.Common.Sentry.DSN != "" {
		err := sentry.Init(sentry.ClientOptions{
			Dsn: cfg.Common.Sentry.DSN,
			BeforeSend: func(event *sentry.Event, _ *sentry.EventHint) *sentry.Event {
				event.Tags["go_version"] = runtime.Version()
				return event
			},
		})
		if err != nil {
			return nil, fmt.Errorf("failed to initialize Sentry: %w", err)
		}
		defer sentry.Flush(2 * time.Second)
	}

	// Logging system is initialized next to capture setup issues
	logManager := logger.NewManager(logDir, &cfg.Common.Debug)
	logger, dbLogger, err := logManager.GetLoggers()
	if err != nil {
		return nil, err
	}

	// Redis manager provides connection pools for various subsystems
	redisManager := redis.NewManager(&cfg.Common.Redis, logger)

	// Database connection pool is created with statistics tracking
	db, err := database.NewConnection(&cfg.Common.PostgreSQL, dbLogger)
	if err != nil {
		return nil, err
	}

	// OpenAI client is configured with API key from config
	genAIClient, err := genai.NewClient(context.Background(), option.WithAPIKey(cfg.Common.GeminiAI.APIKey))
	if err != nil {
		logger.Fatal("Failed to create Gemini client", zap.Error(err))
	}

	// RoAPI client is configured with middleware chain
	roAPI, err := client.GetRoAPIClient(&cfg.Common, redisManager, logger)
	if err != nil {
		return nil, err
	}

	// Queue manager creates its own Redis database for job storage
	queueClient, err := redisManager.GetClient(redis.QueueDBIndex)
	if err != nil {
		return nil, err
	}
	queueManager := queue.NewManager(db, queueClient, logger)

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
		DBLogger:     dbLogger,
		DB:           db,
		GenAIClient:  genAIClient,
		GenAIModel:   cfg.Common.GeminiAI.Model,
		RoAPI:        roAPI,
		Queue:        queueManager,
		RedisManager: redisManager,
		StatusClient: statusClient,
		LogManager:   logManager,
		pprofServer:  pprofSrv,
	}, nil
}

// Cleanup ensures graceful shutdown of all components in reverse initialization order.
// Logs but does not fail on cleanup errors to ensure all components get cleanup attempts.
func (s *App) Cleanup() {
	// Shutdown pprof server if running
	if s.pprofServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

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

	// Close Redis connections last as other components might need it during cleanup
	s.RedisManager.Close()

	// Close Gemini AI client
	s.GenAIClient.Close()
}

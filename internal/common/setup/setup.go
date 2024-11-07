package setup

import (
	"context"
	"log"
	"time"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/rotector/rotector/internal/common/config"
	"github.com/rotector/rotector/internal/common/database"
	"github.com/rotector/rotector/internal/common/queue"
	"github.com/rotector/rotector/internal/common/redis"
	"github.com/rotector/rotector/internal/common/statistics"
	"go.uber.org/zap"
)

// AppSetup bundles all core dependencies and services needed by the application.
// Each field represents a major subsystem that needs initialization and cleanup.
type AppSetup struct {
	Config       *config.Config     // Application configuration
	Logger       *zap.Logger        // Main application logger
	DBLogger     *zap.Logger        // Database-specific logger
	DB           *database.Database // Database connection pool
	OpenAIClient *openai.Client     // OpenAI API client
	Stats        *statistics.Client // Statistics tracking
	RoAPI        *api.API           // RoAPI HTTP client
	Queue        *queue.Manager     // Background job queue
	RedisManager *redis.Manager     // Redis connection manager
	LogManager   *LogManager        // Log management system
	pprofServer  *pprofServer       // Debug HTTP server for pprof
}

// InitializeApp bootstraps all application dependencies in the correct order,
// ensuring each component has its required dependencies available.
func InitializeApp(logDir string) (*AppSetup, error) {
	// Configuration must be loaded first as other components depend on it
	cfg, err := config.LoadConfig()
	if err != nil {
		return nil, err
	}

	// Logging system is initialized next to capture setup issues
	logManager := NewLogManager(logDir, cfg.Debug.LogLevel, cfg.Debug.MaxLogsToKeep)
	logger, dbLogger, err := logManager.GetLoggers()
	if err != nil {
		return nil, err
	}

	// Redis manager provides connection pools for various subsystems
	redisManager := redis.NewManager(cfg, logger)

	// Statistics tracking requires its own Redis database
	statsRedis, err := redisManager.GetClient(redis.StatsDBIndex)
	if err != nil {
		logger.Fatal("Failed to create statistics Redis client", zap.Error(err))
		return nil, err
	}
	stats := statistics.NewClient(statsRedis, logger)

	// Database connection pool is created with statistics tracking
	db, err := database.NewConnection(cfg, stats, dbLogger)
	if err != nil {
		logger.Fatal("Failed to connect to database", zap.Error(err))
		return nil, err
	}

	// OpenAI client is configured with API key from config
	openaiClient := openai.NewClient(
		option.WithAPIKey(cfg.OpenAI.APIKey),
		option.WithRequestTimeout(30*time.Second),
	)

	// RoAPI client is configured with middleware chain
	roAPI, err := getRoAPIClient(cfg, redisManager, logger)
	if err != nil {
		logger.Fatal("Failed to create RoAPI client", zap.Error(err))
		return nil, err
	}

	// Queue manager creates its own Redis database for job storage
	queueRedis, err := redisManager.GetClient(redis.QueueDBIndex)
	if err != nil {
		logger.Fatal("Failed to create queue Redis client", zap.Error(err))
		return nil, err
	}
	queueManager := queue.NewManager(db, queueRedis, logger)

	// Start pprof server if enabled
	var pprofSrv *pprofServer
	if cfg.Debug.EnablePprof {
		srv, err := startPprofServer(cfg.Debug.PprofPort, logger)
		if err != nil {
			logger.Error("Failed to start pprof server", zap.Error(err))
		} else {
			pprofSrv = srv
			logger.Warn("pprof debugging endpoint enabled - this should not be used in production!")
		}
	}

	// Bundle all initialized components
	return &AppSetup{
		Config:       cfg,
		Logger:       logger,
		DBLogger:     dbLogger,
		DB:           db,
		OpenAIClient: openaiClient,
		Stats:        stats,
		RoAPI:        roAPI,
		Queue:        queueManager,
		RedisManager: redisManager,
		LogManager:   logManager,
		pprofServer:  pprofSrv,
	}, nil
}

// CleanupApp ensures graceful shutdown of all components in reverse initialization order.
// Logs but does not fail on cleanup errors to ensure all components get cleanup attempts.
func (s *AppSetup) CleanupApp() {
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
}

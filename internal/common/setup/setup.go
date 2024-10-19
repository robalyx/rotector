package setup

import (
	"fmt"
	"log"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/redis/rueidis"
	"github.com/rotector/rotector/internal/common/config"
	"github.com/rotector/rotector/internal/common/database"
	"github.com/rotector/rotector/internal/common/logging"
	"github.com/rotector/rotector/internal/common/statistics"
	"go.uber.org/zap"
)

// AppSetup contains all the common setup components.
type AppSetup struct {
	Config       *config.Config
	Logger       *zap.Logger
	DBLogger     *zap.Logger
	DB           *database.Database
	RedisClient  rueidis.Client
	OpenAIClient *openai.Client
	RoAPI        *api.API
}

// InitializeApp performs common setup tasks and returns an AppSetup.
func InitializeApp(logDir string) (*AppSetup, error) {
	// Load configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		return nil, err
	}

	// Initialize logging
	logger, dbLogger, err := logging.SetupLogging(logDir, cfg.Logging.Level, cfg.Logging.MaxLogsToKeep)
	if err != nil {
		return nil, err
	}

	// Initialize Redis client
	redisClient, err := rueidis.NewClient(rueidis.ClientOption{
		InitAddress: []string{fmt.Sprintf("%s:%d", cfg.Redis.Host, cfg.Redis.Port)},
		Username:    cfg.Redis.Username,
		Password:    cfg.Redis.Password,
		SelectDB:    statistics.DBIndex,
	})
	if err != nil {
		logger.Fatal("Failed to connect to Redis", zap.Error(err))
		return nil, err
	}

	// Initialize Statistics
	stats := statistics.NewStatistics(redisClient)

	// Initialize database connection
	db, err := database.NewConnection(cfg, stats, dbLogger)
	if err != nil {
		logger.Fatal("Failed to connect to database", zap.Error(err))
		return nil, err
	}

	// Initialize OpenAI client
	openaiClient := openai.NewClient(
		option.WithAPIKey(cfg.OpenAI.APIKey),
	)

	// Initialize Roblox API client
	roAPI := GetRoAPIClient(cfg, cfg.Redis, logger)

	return &AppSetup{
		Config:       cfg,
		Logger:       logger,
		DBLogger:     dbLogger,
		DB:           db,
		RedisClient:  redisClient,
		OpenAIClient: openaiClient,
		RoAPI:        roAPI,
	}, nil
}

// CleanupApp performs cleanup tasks.
func (setup *AppSetup) CleanupApp() {
	if err := setup.Logger.Sync(); err != nil {
		log.Printf("Failed to sync logger: %v", err)
	}
	if err := setup.DBLogger.Sync(); err != nil {
		log.Printf("Failed to sync DB logger: %v", err)
	}
	if err := setup.DB.Close(); err != nil {
		log.Printf("Failed to close database connection: %v", err)
	}
	setup.RedisClient.Close()
}

package setup

import (
	"log"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/rotector/rotector/internal/common/config"
	"github.com/rotector/rotector/internal/common/database"
	"github.com/rotector/rotector/internal/common/logging"
	"go.uber.org/zap"
)

// AppSetup contains all the common setup components.
type AppSetup struct {
	Config       *config.Config
	Logger       *zap.Logger
	DBLogger     *zap.Logger
	DB           *database.Database
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

	// Initialize database connection
	db, err := database.NewConnection(cfg.PostgreSQL, dbLogger)
	if err != nil {
		logger.Fatal("Failed to connect to database", zap.Error(err))
		return nil, err
	}

	// Initialize OpenAI client
	openaiClient := openai.NewClient(
		option.WithAPIKey(cfg.OpenAI.APIKey),
	)

	// Initialize Roblox API client
	roAPI := GetRoAPIClient(cfg.Roblox, cfg.Redis, logger)
	return &AppSetup{
		Config:       cfg,
		Logger:       logger,
		DBLogger:     dbLogger,
		DB:           db,
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
}

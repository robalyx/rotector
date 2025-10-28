package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/robalyx/rotector/cmd/db/commands"
	"github.com/robalyx/rotector/internal/cloudflare"
	"github.com/robalyx/rotector/internal/database"
	"github.com/robalyx/rotector/internal/database/migrations"
	"github.com/robalyx/rotector/internal/redis"
	"github.com/robalyx/rotector/internal/setup/client"
	"github.com/robalyx/rotector/internal/setup/config"
	"github.com/uptrace/bun/migrate"
	"github.com/urfave/cli/v3"
	"go.uber.org/zap"
)

func main() {
	if err := run(); err != nil {
		log.Printf("Error: %v", err)
		os.Exit(1)
	}
}

func run() error {
	// Setup dependencies
	deps, err := setupDependencies()
	if err != nil {
		return fmt.Errorf("failed to setup dependencies: %w", err)
	}
	defer deps.db.Close()

	// Convert dependencies to the commands package format
	cmdDeps := &commands.CLIDependencies{
		DB:       deps.db,
		Migrator: deps.migrator,
		CFClient: deps.cfClient,
		RoAPI:    deps.roAPI,
		Logger:   deps.logger,
	}

	// Collect all commands from different modules
	var allCommands []*cli.Command

	allCommands = append(allCommands, commands.MigrationCommands(cmdDeps)...)
	allCommands = append(allCommands, commands.CleanupCommands(cmdDeps)...)
	allCommands = append(allCommands, commands.AnalysisCommands(cmdDeps)...)
	allCommands = append(allCommands, commands.FriendCleanupCommands(cmdDeps)...)
	allCommands = append(allCommands, commands.GroupCleanupCommands(cmdDeps)...)
	allCommands = append(allCommands, commands.DeletionCommands(cmdDeps)...)

	app := &cli.Command{
		Name:     "db",
		Usage:    "Database management tool",
		Commands: allCommands,
	}

	return app.Run(context.Background(), os.Args)
}

// cliDependencies holds the common dependencies needed by CLI commands.
type cliDependencies struct {
	db       database.Client
	migrator *migrate.Migrator
	cfClient *cloudflare.Client
	roAPI    *api.API
	logger   *zap.Logger
}

// setupDependencies initializes all dependencies needed by the CLI.
func setupDependencies() (*cliDependencies, error) {
	ctx := context.Background()

	// Load full configuration
	cfg, configDir, err := config.LoadConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	// Create development logger
	logger, err := zap.NewDevelopment()
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}

	// Connect to database
	db, err := database.NewConnection(ctx, &cfg.Common.PostgreSQL, logger, false)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Create migrator using database connection and migrations
	migrator := migrate.NewMigrator(db.DB(), migrations.Migrations)

	// Initialize Cloudflare client
	cfClient := cloudflare.NewClient(cfg, db, logger)

	// Initialize Redis manager for RoAPI client
	redisManager := redis.NewManager(&cfg.Common.Redis, logger)

	// Initialize Roblox API client with proper configuration
	roAPI, _, err := client.GetRoAPIClient(ctx, &cfg.Common, configDir, redisManager, logger, 30*time.Second)
	if err != nil {
		db.Close()
		redisManager.Close()

		return nil, fmt.Errorf("failed to initialize RoAPI client: %w", err)
	}

	return &cliDependencies{
		db:       db,
		migrator: migrator,
		cfClient: cfClient,
		roAPI:    roAPI,
		logger:   logger,
	}, nil
}

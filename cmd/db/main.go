package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/robalyx/rotector/internal/bot/utils"
	"github.com/robalyx/rotector/internal/database"
	"github.com/robalyx/rotector/internal/database/migrations"
	"github.com/robalyx/rotector/internal/database/types/enum"
	"github.com/robalyx/rotector/internal/setup/config"
	"github.com/uptrace/bun/migrate"
	"github.com/urfave/cli/v3"
	"go.uber.org/zap"
)

var (
	ErrNameRequired   = errors.New("NAME argument required")
	ErrReasonRequired = errors.New("REASON argument required")
	ErrTimeRequired   = errors.New("TIME argument required")
)

// cliDependencies holds the common dependencies needed by CLI commands.
type cliDependencies struct {
	db       database.Client
	migrator *migrate.Migrator
	logger   *zap.Logger
}

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

	app := &cli.Command{
		Name:  "db",
		Usage: "Database management tool",
		Commands: []*cli.Command{
			{
				Name:   "init",
				Usage:  "Initialize migration tables",
				Action: handleInit(deps),
			},
			{
				Name:   "migrate",
				Usage:  "Run pending migrations",
				Action: handleMigrate(deps),
			},
			{
				Name:   "rollback",
				Usage:  "Rollback the last migration group",
				Action: handleRollback(deps),
			},
			{
				Name:   "status",
				Usage:  "Show migration status",
				Action: handleStatus(deps),
			},
			{
				Name:      "create",
				Usage:     "Create a new Go migration file",
				ArgsUsage: "NAME",
				Action:    handleCreate(deps),
			},
			{
				Name:      "clear-reason",
				Usage:     "Clear flagged users with only a specific reason type",
				ArgsUsage: "REASON",
				Flags: []cli.Flag{
					&cli.IntFlag{
						Name:    "batch-size",
						Usage:   "Number of users to process in each batch",
						Value:   5000,
						Aliases: []string{"b"},
					},
				},
				Action: handleClearReason(deps),
			},
			{
				Name:      "delete-after-time",
				Usage:     "Delete flagged users that have been updated after a specific time",
				ArgsUsage: "TIME",
				Description: `Delete flagged users that have been updated after the specified time.
				
TIME can be in various formats:
  - "2006-01-02" (date only, assumes 00:00:00 UTC)
  - "2006-01-02 15:04:05" (datetime, assumes UTC)
  - "2006-01-02 15:04:05 UTC" (datetime with UTC timezone)
  - "2006-01-02 15:04:05 America/New_York" (datetime with timezone)
  - "2006-01-02T15:04:05Z" (RFC3339 format)
  - "2006-01-02T15:04:05-07:00" (RFC3339 with timezone offset)

Examples:
  db delete-after-time "2024-01-01"
  db delete-after-time "2024-01-01 12:00:00"
  db delete-after-time "2024-01-01T12:00:00Z"
  db delete-after-time "2024-01-01T12:00:00+08:00"

Note: When using timezone names with spaces (like "Asia/Singapore"), you may need 
to escape quotes depending on your shell:
  just run-db delete-after-time '"2024-01-01 12:00:00 Asia/Singapore"'
  
For reliable cross-platform usage, prefer RFC3339 format with timezone offsets.`,
				Flags: []cli.Flag{
					&cli.IntFlag{
						Name:    "batch-size",
						Usage:   "Number of users to process in each batch",
						Value:   5000,
						Aliases: []string{"b"},
					},
				},
				Action: handleDeleteAfterTime(deps),
			},
		},
	}

	return app.Run(context.Background(), os.Args)
}

// setupDependencies initializes all dependencies needed by the CLI.
func setupDependencies() (*cliDependencies, error) {
	// Load full configuration
	cfg, _, err := config.LoadConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	// Create development logger
	logger, err := zap.NewDevelopment()
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}

	// Connect to database
	db, err := database.NewConnection(context.Background(), &cfg.Common.PostgreSQL, logger, false)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Create migrator using database connection and migrations
	migrator := migrate.NewMigrator(db.DB(), migrations.Migrations)

	return &cliDependencies{
		db:       db,
		migrator: migrator,
		logger:   logger,
	}, nil
}

// handleInit handles the 'init' command.
func handleInit(deps *cliDependencies) cli.ActionFunc {
	return func(ctx context.Context, _ *cli.Command) error {
		return deps.migrator.Init(ctx)
	}
}

// handleMigrate handles the 'migrate' command.
func handleMigrate(deps *cliDependencies) cli.ActionFunc {
	return func(ctx context.Context, _ *cli.Command) error {
		if err := deps.migrator.Lock(ctx); err != nil {
			return err
		}
		defer deps.migrator.Unlock(ctx) //nolint:errcheck // -

		group, err := deps.migrator.Migrate(ctx)
		if err != nil {
			return err
		}

		if group.IsZero() {
			deps.logger.Info("No new migrations to run (database is up to date)")
			return nil
		}

		deps.logger.Info("Successfully migrated",
			zap.String("group", group.String()),
		)
		return nil
	}
}

// handleRollback handles the 'rollback' command.
func handleRollback(deps *cliDependencies) cli.ActionFunc {
	return func(ctx context.Context, _ *cli.Command) error {
		if err := deps.migrator.Lock(ctx); err != nil {
			return err
		}
		defer deps.migrator.Unlock(ctx) //nolint:errcheck // -

		group, err := deps.migrator.Rollback(ctx)
		if err != nil {
			return err
		}

		if group.IsZero() {
			deps.logger.Info("No groups to roll back")
			return nil
		}

		deps.logger.Info("Successfully rolled back",
			zap.String("group", group.String()),
		)
		return nil
	}
}

// handleStatus handles the 'status' command.
func handleStatus(deps *cliDependencies) cli.ActionFunc {
	return func(ctx context.Context, _ *cli.Command) error {
		ms, err := deps.migrator.MigrationsWithStatus(ctx)
		if err != nil {
			return err
		}

		deps.logger.Info("Migration status",
			zap.String("migrations", ms.String()),
			zap.String("unapplied", ms.Unapplied().String()),
			zap.String("last_group", ms.LastGroup().String()),
		)
		return nil
	}
}

// handleCreate handles the 'create' command.
func handleCreate(deps *cliDependencies) cli.ActionFunc {
	return func(ctx context.Context, c *cli.Command) error {
		if c.Args().Len() != 1 {
			return ErrNameRequired
		}

		mf, err := deps.migrator.CreateGoMigration(ctx, c.Args().First())
		if err != nil {
			return err
		}

		deps.logger.Info("Created Go migration",
			zap.String("name", mf.Name),
			zap.String("path", mf.Path),
		)
		return nil
	}
}

// handleClearReason handles the 'clear-reason' command.
func handleClearReason(deps *cliDependencies) cli.ActionFunc {
	return func(ctx context.Context, c *cli.Command) error {
		if c.Args().Len() != 1 {
			return ErrReasonRequired
		}

		// Get batch size from flag
		batchSize := max(c.Int("batch-size"), 5000)

		// Get reason type from argument
		reasonStr := strings.ToUpper(c.Args().First())
		reasonType, err := enum.UserReasonTypeString(reasonStr)
		if err != nil {
			return fmt.Errorf("invalid reason type %q: %w", reasonStr, err)
		}

		// Get users with only this reason
		users, err := deps.db.Model().User().GetFlaggedUsersWithOnlyReason(ctx, reasonType)
		if err != nil {
			return fmt.Errorf("failed to get users: %w", err)
		}

		if len(users) == 0 {
			deps.logger.Info("No users found with only the specified reason",
				zap.String("reason", reasonType.String()))
			return nil
		}

		// Ask for confirmation
		deps.logger.Info("Found users to clear",
			zap.Int("count", len(users)),
			zap.String("reason", reasonType.String()))

		log.Printf("Are you sure you want to delete these %d users in batches of %d? (y/N)",
			len(users), batchSize)
		var response string
		_, _ = fmt.Scanln(&response)

		if response != "y" && response != "Y" {
			deps.logger.Info("Operation cancelled")
			return nil
		}

		// Create user ID slices
		userIDs := make([]uint64, len(users))
		for i, user := range users {
			userIDs[i] = user.ID
		}

		// Process in batches
		var totalAffected int64
		var totalProcessed int

		for i := 0; i < len(userIDs); i += batchSize {
			end := min(i+batchSize, len(userIDs))

			batchIDs := userIDs[i:end]
			batchCount := len(batchIDs)

			deps.logger.Info("Processing batch",
				zap.Int("batch", i/batchSize+1),
				zap.Int("size", batchCount),
				zap.Int("processed", totalProcessed),
				zap.Int("remaining", len(userIDs)-totalProcessed))

			affected, err := deps.db.Service().User().DeleteUsers(ctx, batchIDs)
			if err != nil {
				return fmt.Errorf("failed to delete users in batch %d: %w", i/batchSize+1, err)
			}

			totalAffected += affected
			totalProcessed += batchCount

			deps.logger.Info("Batch processed successfully",
				zap.Int("batch", i/batchSize+1),
				zap.Int("processed", batchCount),
				zap.Int64("affected_rows", affected))

			// Add a small delay between batches to reduce database load
			if end < len(userIDs) {
				time.Sleep(100 * time.Millisecond)
			}
		}

		deps.logger.Info("Successfully cleared all users",
			zap.Int("total_count", len(users)),
			zap.Int64("total_affected_rows", totalAffected))

		return nil
	}
}

// handleDeleteAfterTime handles the 'delete-after-time' command.
func handleDeleteAfterTime(deps *cliDependencies) cli.ActionFunc {
	return func(ctx context.Context, c *cli.Command) error {
		if c.Args().Len() != 1 {
			return ErrTimeRequired
		}

		// Get batch size from flag
		batchSize := max(c.Int("batch-size"), 1000)

		// Parse the time string with timezone support
		timeStr := c.Args().First()
		cutoffTime, err := utils.ParseTimeWithTimezone(timeStr)
		if err != nil {
			return fmt.Errorf("failed to parse time %q: %w", timeStr, err)
		}

		deps.logger.Info("Parsed cutoff time",
			zap.String("input", timeStr),
			zap.Time("cutoffTime", cutoffTime),
			zap.String("timezone", cutoffTime.Location().String()))

		// Get users updated after the cutoff time
		users, err := deps.db.Model().User().GetUsersUpdatedAfter(ctx, cutoffTime)
		if err != nil {
			return fmt.Errorf("failed to get users: %w", err)
		}

		if len(users) == 0 {
			deps.logger.Info("No flagged users found updated after the specified time",
				zap.Time("cutoffTime", cutoffTime))
			return nil
		}

		// Ask for confirmation
		deps.logger.Info("Found flagged users to delete",
			zap.Int("count", len(users)),
			zap.Time("cutoffTime", cutoffTime),
			zap.String("timezone", cutoffTime.Location().String()))

		log.Printf("Are you sure you want to delete these %d flagged users updated after %s in batches of %d? (y/N)",
			len(users), cutoffTime.Format("2006-01-02 15:04:05 MST"), batchSize)
		var response string
		_, _ = fmt.Scanln(&response)

		if response != "y" && response != "Y" {
			deps.logger.Info("Operation cancelled")
			return nil
		}

		// Create user ID slices
		userIDs := make([]uint64, len(users))
		for i, user := range users {
			userIDs[i] = user.ID
		}

		// Process in batches
		var totalAffected int64
		var totalProcessed int

		for i := 0; i < len(userIDs); i += batchSize {
			end := min(i+batchSize, len(userIDs))

			batchIDs := userIDs[i:end]
			batchCount := len(batchIDs)

			deps.logger.Info("Processing batch",
				zap.Int("batch", i/batchSize+1),
				zap.Int("size", batchCount),
				zap.Int("processed", totalProcessed),
				zap.Int("remaining", len(userIDs)-totalProcessed))

			affected, err := deps.db.Service().User().DeleteUsers(ctx, batchIDs)
			if err != nil {
				return fmt.Errorf("failed to delete users in batch %d: %w", i/batchSize+1, err)
			}

			totalAffected += affected
			totalProcessed += batchCount

			deps.logger.Info("Batch processed successfully",
				zap.Int("batch", i/batchSize+1),
				zap.Int("processed", batchCount),
				zap.Int64("affected_rows", affected))

			// Add a small delay between batches to reduce database load
			if end < len(userIDs) {
				time.Sleep(100 * time.Millisecond)
			}
		}

		deps.logger.Info("Successfully deleted all flagged users",
			zap.Int("total_count", len(users)),
			zap.Int64("total_affected_rows", totalAffected),
			zap.Time("cutoffTime", cutoffTime))

		return nil
	}
}

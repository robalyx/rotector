package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/rotector/rotector/internal/common/setup/config"
	"github.com/rotector/rotector/internal/common/storage/database"
	"github.com/rotector/rotector/internal/common/storage/database/migrations"
	"github.com/uptrace/bun/migrate"
	"github.com/urfave/cli/v3"
	"go.uber.org/zap"
)

var ErrNameRequired = errors.New("NAME argument required")

func main() {
	if err := run(); err != nil {
		log.Printf("Error: %v", err)
		os.Exit(1)
	}
}

func run() error {
	// Setup dependencies
	db, migrator, logger, err := setupMigrator()
	if err != nil {
		return fmt.Errorf("failed to setup migrator: %w", err)
	}
	defer db.Close()

	app := &cli.Command{
		Name:  "db",
		Usage: "Database management tool",
		Commands: []*cli.Command{
			{
				Name:  "init",
				Usage: "Initialize migration tables",
				Action: func(ctx context.Context, _ *cli.Command) error {
					return migrator.Init(ctx)
				},
			},
			{
				Name:  "migrate",
				Usage: "Run pending migrations",
				Action: func(ctx context.Context, _ *cli.Command) error {
					if err := migrator.Lock(ctx); err != nil {
						return err
					}
					defer migrator.Unlock(ctx) //nolint:errcheck

					group, err := migrator.Migrate(ctx)
					if err != nil {
						return err
					}

					if group.IsZero() {
						logger.Info("No new migrations to run (database is up to date)")
						return nil
					}

					logger.Info("Successfully migrated",
						zap.String("group", group.String()),
					)
					return nil
				},
			},
			{
				Name:  "rollback",
				Usage: "Rollback the last migration group",
				Action: func(ctx context.Context, _ *cli.Command) error {
					if err := migrator.Lock(ctx); err != nil {
						return err
					}
					defer migrator.Unlock(ctx) //nolint:errcheck

					group, err := migrator.Rollback(ctx)
					if err != nil {
						return err
					}

					if group.IsZero() {
						logger.Info("No groups to roll back")
						return nil
					}

					logger.Info("Successfully rolled back",
						zap.String("group", group.String()),
					)
					return nil
				},
			},
			{
				Name:  "status",
				Usage: "Show migration status",
				Action: func(ctx context.Context, _ *cli.Command) error {
					ms, err := migrator.MigrationsWithStatus(ctx)
					if err != nil {
						return err
					}

					logger.Info("Migration status",
						zap.String("migrations", ms.String()),
						zap.String("unapplied", ms.Unapplied().String()),
						zap.String("last_group", ms.LastGroup().String()),
					)
					return nil
				},
			},
			{
				Name:      "create",
				Usage:     "Create a new Go migration file",
				ArgsUsage: "NAME",
				Action: func(ctx context.Context, c *cli.Command) error {
					if c.Args().Len() != 1 {
						return ErrNameRequired
					}

					mf, err := migrator.CreateGoMigration(ctx, c.Args().First())
					if err != nil {
						return err
					}

					logger.Info("Created Go migration",
						zap.String("name", mf.Name),
						zap.String("path", mf.Path),
					)
					return nil
				},
			},
		},
	}

	return app.Run(context.Background(), os.Args)
}

// setupMigrator initializes the database connection and migrator.
func setupMigrator() (*database.Client, *migrate.Migrator, *zap.Logger, error) {
	// Load full configuration
	cfg, _, err := config.LoadConfig()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to load config: %w", err)
	}

	// Create development logger
	logger, err := zap.NewDevelopment()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create logger: %w", err)
	}

	// Connect to database
	db, err := database.NewConnection(context.Background(), &cfg.Common.PostgreSQL, logger, false)
	if err != nil {
		return nil, nil, logger, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Create migrator using database connection and migrations
	migrator := migrate.NewMigrator(db.DB(), migrations.Migrations)

	return db, migrator, logger, nil
}

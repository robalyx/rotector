package main

import (
	"fmt"
	"log"

	"github.com/rotector/rotector/internal/common/config"
	"github.com/rotector/rotector/internal/common/storage/database"
	"github.com/rotector/rotector/internal/common/storage/database/migrations"
	"github.com/spf13/cobra"
	"github.com/uptrace/bun/migrate"
	"go.uber.org/zap"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		log.Fatal(err)
	}
}

func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "db",
		Short: "Database management tool",
		Long:  "Tool for managing database migrations and maintenance tasks.",
	}

	// Add subcommands
	rootCmd.AddCommand(
		newInitCmd(),
		newMigrateCmd(),
		newRollbackCmd(),
		newStatusCmd(),
		newCreateCmd(),
	)

	return rootCmd
}

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Initialize migration tables",
		RunE: func(cmd *cobra.Command, _ []string) error {
			db, migrator, _, err := setupMigrator()
			if err != nil {
				return err
			}
			defer db.Close()

			return migrator.Init(cmd.Context())
		},
	}
}

func newMigrateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "migrate",
		Short: "Run pending migrations",
		RunE: func(cmd *cobra.Command, _ []string) error {
			db, migrator, logger, err := setupMigrator()
			if err != nil {
				return err
			}
			defer db.Close()

			if err := migrator.Lock(cmd.Context()); err != nil {
				return err
			}
			defer migrator.Unlock(cmd.Context()) //nolint:errcheck

			group, err := migrator.Migrate(cmd.Context())
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
	}
}

func newRollbackCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rollback",
		Short: "Rollback the last migration group",
		RunE: func(cmd *cobra.Command, _ []string) error {
			db, migrator, logger, err := setupMigrator()
			if err != nil {
				return err
			}
			defer db.Close()

			if err := migrator.Lock(cmd.Context()); err != nil {
				return err
			}
			defer migrator.Unlock(cmd.Context()) //nolint:errcheck

			group, err := migrator.Rollback(cmd.Context())
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
	}
}

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show migration status",
		RunE: func(cmd *cobra.Command, _ []string) error {
			db, migrator, logger, err := setupMigrator()
			if err != nil {
				return err
			}
			defer db.Close()

			ms, err := migrator.MigrationsWithStatus(cmd.Context())
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
	}
}

func newCreateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "create [name]",
		Short: "Create a new Go migration file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			db, migrator, logger, err := setupMigrator()
			if err != nil {
				return err
			}
			defer db.Close()

			mf, err := migrator.CreateGoMigration(cmd.Context(), args[0])
			if err != nil {
				return err
			}

			logger.Info("Created Go migration",
				zap.String("name", mf.Name),
				zap.String("path", mf.Path),
			)
			return nil
		},
	}
}

// setupMigrator initializes the database connection and migrator.
func setupMigrator() (*database.Client, *migrate.Migrator, *zap.Logger, error) {
	// Load full configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to load config: %w", err)
	}

	// Create development logger
	logger, err := zap.NewDevelopment()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create logger: %w", err)
	}

	// Connect to database
	db, err := database.NewConnection(&cfg.Common.PostgreSQL, logger)
	if err != nil {
		return nil, nil, logger, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Create migrator using database connection and migrations
	migrator := migrate.NewMigrator(db.DB(), migrations.Migrations)

	return db, migrator, logger, nil
}

package commands

import (
	"context"

	"github.com/urfave/cli/v3"
	"go.uber.org/zap"
)

// MigrationCommands returns all migration-related commands.
func MigrationCommands(deps *CLIDependencies) []*cli.Command {
	return []*cli.Command{
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
	}
}

// handleInit handles the 'init' command.
func handleInit(deps *CLIDependencies) cli.ActionFunc {
	return func(ctx context.Context, _ *cli.Command) error {
		return deps.Migrator.Init(ctx)
	}
}

// handleMigrate handles the 'migrate' command.
func handleMigrate(deps *CLIDependencies) cli.ActionFunc {
	return func(ctx context.Context, _ *cli.Command) error {
		if err := deps.Migrator.Lock(ctx); err != nil {
			return err
		}
		defer deps.Migrator.Unlock(ctx) //nolint:errcheck // -

		group, err := deps.Migrator.Migrate(ctx)
		if err != nil {
			return err
		}

		if group.IsZero() {
			deps.Logger.Info("No new migrations to run (database is up to date)")
			return nil
		}

		deps.Logger.Info("Successfully migrated",
			zap.String("group", group.String()),
		)

		return nil
	}
}

// handleRollback handles the 'rollback' command.
func handleRollback(deps *CLIDependencies) cli.ActionFunc {
	return func(ctx context.Context, _ *cli.Command) error {
		if err := deps.Migrator.Lock(ctx); err != nil {
			return err
		}
		defer deps.Migrator.Unlock(ctx) //nolint:errcheck // -

		group, err := deps.Migrator.Rollback(ctx)
		if err != nil {
			return err
		}

		if group.IsZero() {
			deps.Logger.Info("No groups to roll back")
			return nil
		}

		deps.Logger.Info("Successfully rolled back",
			zap.String("group", group.String()),
		)

		return nil
	}
}

// handleStatus handles the 'status' command.
func handleStatus(deps *CLIDependencies) cli.ActionFunc {
	return func(ctx context.Context, _ *cli.Command) error {
		ms, err := deps.Migrator.MigrationsWithStatus(ctx)
		if err != nil {
			return err
		}

		deps.Logger.Info("Migration status",
			zap.String("migrations", ms.String()),
			zap.String("unapplied", ms.Unapplied().String()),
			zap.String("last_group", ms.LastGroup().String()),
		)

		return nil
	}
}

// handleCreate handles the 'create' command.
func handleCreate(deps *CLIDependencies) cli.ActionFunc {
	return func(ctx context.Context, c *cli.Command) error {
		if c.Args().Len() != 1 {
			return ErrNameRequired
		}

		mf, err := deps.Migrator.CreateGoMigration(ctx, c.Args().First())
		if err != nil {
			return err
		}

		deps.Logger.Info("Created Go migration",
			zap.String("name", mf.Name),
			zap.String("path", mf.Path),
		)

		return nil
	}
}

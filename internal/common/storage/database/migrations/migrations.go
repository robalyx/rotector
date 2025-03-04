package migrations

import (
	"github.com/uptrace/bun/migrate"
)

// Migrations holds all database migrations.
var Migrations = migrate.NewMigrations() //nolint:gochecknoglobals // -

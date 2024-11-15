package database

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/rotector/rotector/internal/common/config"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
	"go.uber.org/zap"
)

// Database represents the database connection and operations.
// It manages access to different repositories that handle specific data types.
type Database struct {
	db           *bun.DB
	logger       *zap.Logger
	users        *UserRepository
	groups       *GroupRepository
	stats        *StatsRepository
	settings     *SettingRepository
	userActivity *UserActivityRepository
	tracking     *TrackingRepository
}

// NewConnection establishes a new database connection and returns a Database instance.
func NewConnection(config *config.Config, logger *zap.Logger) (*Database, error) {
	// Initialize database connection with config values
	sqldb := sql.OpenDB(pgdriver.NewConnector(
		pgdriver.WithAddr(fmt.Sprintf("%s:%d", config.PostgreSQL.Host, config.PostgreSQL.Port)),
		pgdriver.WithUser(config.PostgreSQL.User),
		pgdriver.WithPassword(config.PostgreSQL.Password),
		pgdriver.WithDatabase(config.PostgreSQL.DBName),
		pgdriver.WithInsecure(true),
	))

	// Create Bun db instance
	db := bun.NewDB(sqldb, pgdialect.New())

	// Enable query logging with zap logger
	if config.Debug.QueryLogging {
		db.AddQueryHook(NewQueryHook(logger))
	}

	// Create repositories
	tracking := NewTrackingRepository(db, logger)
	database := &Database{
		db:           db,
		logger:       logger,
		users:        NewUserRepository(db, tracking, logger),
		groups:       NewGroupRepository(db, logger),
		stats:        NewStatsRepository(db, logger),
		settings:     NewSettingRepository(db, logger),
		userActivity: NewUserActivityRepository(db, logger),
		tracking:     tracking,
	}

	// Initialize database schema and TimescaleDB extension
	if err := database.createSchema(); err != nil {
		return nil, fmt.Errorf("failed to create schema: %w", err)
	}

	if err := database.setupTimescaleDB(); err != nil {
		return nil, fmt.Errorf("failed to setup TimescaleDB: %w", err)
	}

	logger.Info("Database connection established and setup completed")
	return database, nil
}

// createSchema creates all required database tables and indexes.
func (d *Database) createSchema() error {
	models := []interface{}{
		(*FlaggedGroup)(nil),
		(*ConfirmedGroup)(nil),
		(*FlaggedUser)(nil),
		(*ConfirmedUser)(nil),
		(*ClearedUser)(nil),
		(*BannedUser)(nil),
		(*HourlyStats)(nil),
		(*UserSetting)(nil),
		(*GuildSetting)(nil),
		(*UserActivityLog)(nil),
		(*GroupMemberTracking)(nil),
	}

	// Create tables if they don't exist
	for _, model := range models {
		_, err := d.db.NewCreateTable().
			Model(model).
			IfNotExists().
			Exec(context.Background())
		if err != nil {
			d.logger.Error("Failed to create table",
				zap.Error(err),
				zap.String("model", fmt.Sprintf("%T", model)))
			return err
		}
	}

	// Create indexes for efficient querying
	_, err := d.db.NewRaw(`
		-- User activity logs indexes
		CREATE INDEX IF NOT EXISTS idx_user_activity_logs_user_id ON user_activity_logs (user_id);
		CREATE INDEX IF NOT EXISTS idx_user_activity_logs_reviewer_id ON user_activity_logs (reviewer_id);
		CREATE INDEX IF NOT EXISTS idx_user_activity_logs_activity_timestamp ON user_activity_logs (activity_timestamp);

		-- Group tracking indexes
		CREATE INDEX IF NOT EXISTS idx_group_member_trackings_last_appended ON group_member_trackings (last_appended);
		CREATE INDEX IF NOT EXISTS idx_group_member_trackings_group_id_array_length 
		ON group_member_trackings USING btree (group_id, array_length(confirmed_users, 1));

		-- User status indexes
		CREATE INDEX IF NOT EXISTS idx_cleared_users_cleared_at ON cleared_users (cleared_at);
		CREATE INDEX IF NOT EXISTS idx_banned_users_purged_at ON banned_users (purged_at);
		CREATE INDEX IF NOT EXISTS idx_flagged_users_last_purge_check ON flagged_users (last_purge_check);
		CREATE INDEX IF NOT EXISTS idx_confirmed_users_last_scanned ON confirmed_users (last_scanned);
		CREATE INDEX IF NOT EXISTS idx_flagged_users_last_viewed ON flagged_users (last_viewed);
		CREATE INDEX IF NOT EXISTS idx_flagged_users_confidence ON flagged_users (confidence DESC);
		CREATE INDEX IF NOT EXISTS idx_flagged_users_last_updated ON flagged_users (last_updated ASC);

		-- Training mode reputation indexes
		CREATE INDEX IF NOT EXISTS idx_flagged_users_reputation ON flagged_users (reputation DESC);
		CREATE INDEX IF NOT EXISTS idx_confirmed_users_reputation ON confirmed_users (reputation DESC);
		CREATE INDEX IF NOT EXISTS idx_cleared_users_reputation ON cleared_users (reputation DESC);
		CREATE INDEX IF NOT EXISTS idx_banned_users_reputation ON banned_users (reputation DESC);

		-- Statistics indexes
		CREATE INDEX IF NOT EXISTS idx_hourly_stats_timestamp ON hourly_stats (timestamp DESC);
	`).Exec(context.Background())
	if err != nil {
		d.logger.Error("Failed to create indexes", zap.Error(err))
		return err
	}
	d.logger.Info("Indexes created or already exist")

	return nil
}

// setupTimescaleDB initializes the TimescaleDB extension and creates hypertables
// for time-series data. This enables efficient querying of historical data.
func (d *Database) setupTimescaleDB() error {
	// Check if TimescaleDB extension exists
	var exists bool
	err := d.db.NewRaw(`
		SELECT EXISTS (
			SELECT 1 
			FROM pg_extension
			WHERE extname = 'timescaledb'
		)
	`).Scan(context.Background(), &exists)
	if err != nil {
		return fmt.Errorf("failed to check TimescaleDB extension: %w", err)
	}

	// Create extension if needed
	if !exists {
		_, err = d.db.NewRaw(`CREATE EXTENSION IF NOT EXISTS timescaledb`).
			Exec(context.Background())
		if err != nil {
			return fmt.Errorf("failed to create TimescaleDB extension: %w", err)
		}
		d.logger.Info("TimescaleDB extension created")
	} else {
		d.logger.Info("TimescaleDB extension already exists")
	}

	// Create hypertable for time-series data
	_, err = d.db.NewRaw(`
		SELECT create_hypertable('user_activity_logs', 'activity_timestamp', if_not_exists => TRUE)
	`).Exec(context.Background())
	if err != nil {
		return fmt.Errorf("failed to create hypertable: %w", err)
	}

	return nil
}

// Close gracefully shuts down the database connection.
func (d *Database) Close() error {
	err := d.db.Close()
	if err != nil {
		d.logger.Error("Failed to close database connection", zap.Error(err))
		return err
	}
	d.logger.Info("Database connection closed")
	return nil
}

// Users returns the repository for user-related operations.
func (d *Database) Users() *UserRepository {
	return d.users
}

// Groups returns the repository for group-related operations.
func (d *Database) Groups() *GroupRepository {
	return d.groups
}

// Stats returns the repository for statistics operations.
func (d *Database) Stats() *StatsRepository {
	return d.stats
}

// Settings returns the repository for user and guild settings.
func (d *Database) Settings() *SettingRepository {
	return d.settings
}

// Tracking returns the repository for tracking user and group relationships.
func (d *Database) Tracking() *TrackingRepository {
	return d.tracking
}

// UserActivity returns the repository for logging user actions.
func (d *Database) UserActivity() *UserActivityRepository {
	return d.userActivity
}

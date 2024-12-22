package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/rotector/rotector/internal/common/config"
	"github.com/rotector/rotector/internal/common/storage/database/models"
	"github.com/rotector/rotector/internal/common/storage/database/types"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
	"go.uber.org/zap"
)

// PartitionCount is the number of partitions for user and group tables.
const PartitionCount = 8

// Client represents the database connection and operations.
// It manages access to different repositories that handle specific data types.
type Client struct {
	db           *bun.DB
	logger       *zap.Logger
	users        *models.UserModel
	groups       *models.GroupModel
	stats        *models.StatsModel
	settings     *models.SettingModel
	userActivity *models.ActivityModel
	tracking     *models.TrackingModel
	appeals      *models.AppealModel
}

// NewConnection establishes a new database connection and returns a Client instance.
func NewConnection(config *config.PostgreSQL, logger *zap.Logger) (*Client, error) {
	// Initialize database connection with config values
	sqldb := sql.OpenDB(pgdriver.NewConnector(
		pgdriver.WithAddr(fmt.Sprintf("%s:%d", config.Host, config.Port)),
		pgdriver.WithUser(config.User),
		pgdriver.WithPassword(config.Password),
		pgdriver.WithDatabase(config.DBName),
		pgdriver.WithInsecure(true),
		pgdriver.WithApplicationName("rotector"),
	))

	// Set connection pool settings
	sqldb.SetMaxOpenConns(config.MaxOpenConns)
	sqldb.SetMaxIdleConns(config.MaxIdleConns)
	sqldb.SetConnMaxLifetime(time.Duration(config.MaxLifetime) * time.Minute)
	sqldb.SetConnMaxIdleTime(time.Duration(config.MaxIdleTime) * time.Minute)

	// Create Bun db instance
	db := bun.NewDB(sqldb, pgdialect.New())

	// Enable query logging with zap logger
	db.AddQueryHook(NewHook(logger))

	// Create repositories
	tracking := models.NewTracking(db, logger)
	client := &Client{
		db:           db,
		logger:       logger,
		users:        models.NewUser(db, tracking, logger),
		groups:       models.NewGroup(db, logger),
		stats:        models.NewStats(db, logger),
		settings:     models.NewSetting(db, logger),
		userActivity: models.NewUserActivity(db, logger),
		tracking:     tracking,
		appeals:      models.NewAppeal(db, logger),
	}

	// Initialize database components
	if err := client.createSchema(); err != nil {
		return nil, fmt.Errorf("failed to create schema: %w", err)
	}

	if err := client.createIndexes(); err != nil {
		return nil, fmt.Errorf("failed to create indexes: %w", err)
	}

	if err := client.setupSequences(); err != nil {
		return nil, fmt.Errorf("failed to setup sequences: %w", err)
	}

	if err := client.setupTimescaleDB(); err != nil {
		return nil, fmt.Errorf("failed to setup TimescaleDB: %w", err)
	}

	logger.Info("Database connection established and setup completed")
	return client, nil
}

// createSchema creates all required database tables.
func (c *Client) createSchema() error {
	// First create parent partitioned tables
	err := c.createPartitionedTables()
	if err != nil {
		return fmt.Errorf("failed to create partitioned tables: %w", err)
	}

	// Then create other non-partitioned tables
	models := []interface{}{
		(*types.HourlyStats)(nil),
		(*types.UserSetting)(nil),
		(*types.BotSetting)(nil),
		(*types.UserActivityLog)(nil),
		(*types.Appeal)(nil),
		(*types.AppealMessage)(nil),
		(*types.AppealTimeline)(nil),
		(*types.GroupMemberTracking)(nil),
	}

	// Create tables if they don't exist
	for _, model := range models {
		_, err := c.db.NewCreateTable().
			Model(model).
			IfNotExists().
			Exec(context.Background())
		if err != nil {
			c.logger.Error("Failed to create table",
				zap.Error(err),
				zap.String("model", fmt.Sprintf("%T", model)))
			return err
		}
	}

	return nil
}

// createPartitionedTables creates the parent tables and their partitions for user and group tables.
func (c *Client) createPartitionedTables() error {
	tables := []struct {
		model interface{}
		name  string
	}{
		{(*types.FlaggedUser)(nil), "flagged_users"},
		{(*types.ConfirmedUser)(nil), "confirmed_users"},
		{(*types.ClearedUser)(nil), "cleared_users"},
		{(*types.BannedUser)(nil), "banned_users"},
		{(*types.FlaggedGroup)(nil), "flagged_groups"},
		{(*types.ConfirmedGroup)(nil), "confirmed_groups"},
		{(*types.ClearedGroup)(nil), "cleared_groups"},
		{(*types.LockedGroup)(nil), "locked_groups"},
	}

	for _, table := range tables {
		// Create partitioned table from model
		_, err := c.db.NewCreateTable().
			Model(table.model).
			ModelTableExpr(table.name).
			IfNotExists().
			PartitionBy(`HASH (id)`).
			Exec(context.Background())
		if err != nil {
			return fmt.Errorf("failed to create parent table %s: %w", table.name, err)
		}

		// Create partitions
		for i := range PartitionCount {
			_, err = c.db.NewRaw(fmt.Sprintf(`
				CREATE TABLE IF NOT EXISTS %s_%d 
				PARTITION OF %s 
				FOR VALUES WITH (modulus %d, remainder %d);
			`, table.name, i, table.name, PartitionCount, i)).Exec(context.Background())
			if err != nil {
				return fmt.Errorf("failed to create partition %s_%d: %w", table.name, i, err)
			}
		}
	}

	return nil
}

// createIndexes creates all database indexes for efficient querying.
func (c *Client) createIndexes() error {
	_, err := c.db.NewRaw(`
		-- User activity logs indexes
		CREATE INDEX IF NOT EXISTS idx_user_activity_logs_time 
		ON user_activity_logs (activity_timestamp DESC, sequence DESC);

		CREATE INDEX IF NOT EXISTS idx_user_activity_logs_user_time 
		ON user_activity_logs (user_id, activity_timestamp DESC, sequence DESC) 
		WHERE user_id > 0;
		
		CREATE INDEX IF NOT EXISTS idx_user_activity_logs_group_time 
		ON user_activity_logs (group_id, activity_timestamp DESC, sequence DESC) 
		WHERE group_id > 0;
		
		CREATE INDEX IF NOT EXISTS idx_user_activity_logs_reviewer_time 
		ON user_activity_logs (reviewer_id, activity_timestamp DESC, sequence DESC);
		
		CREATE INDEX IF NOT EXISTS idx_user_activity_logs_type_time 
		ON user_activity_logs (activity_type, activity_timestamp DESC, sequence DESC);

		-- Appeal indexes
		CREATE INDEX IF NOT EXISTS idx_appeals_user_id ON appeals (user_id);
		CREATE INDEX IF NOT EXISTS idx_appeals_requester_id ON appeals (requester_id);
		CREATE INDEX IF NOT EXISTS idx_appeals_status ON appeals (status);
		CREATE INDEX IF NOT EXISTS idx_appeals_claimed_by ON appeals (claimed_by) WHERE claimed_by > 0;

		-- Appeal timeline indexes
		CREATE INDEX IF NOT EXISTS idx_appeal_timelines_timestamp_asc 
		ON appeal_timelines (timestamp ASC, id ASC);

		CREATE INDEX IF NOT EXISTS idx_appeal_timelines_timestamp_desc 
		ON appeal_timelines (timestamp DESC, id DESC);

		CREATE INDEX IF NOT EXISTS idx_appeal_timelines_activity_desc
		ON appeal_timelines (last_activity DESC, id DESC);

		-- Appeal messages index
		CREATE INDEX IF NOT EXISTS idx_appeal_messages_appeal_created
		ON appeal_messages (appeal_id, created_at ASC);

		-- Scanning indexes for users and groups
		CREATE INDEX IF NOT EXISTS idx_confirmed_users_scan_time ON confirmed_users (last_scanned ASC);
		CREATE INDEX IF NOT EXISTS idx_flagged_users_scan_time ON flagged_users (last_scanned ASC);
		CREATE INDEX IF NOT EXISTS idx_confirmed_groups_scan_time ON confirmed_groups (last_scanned ASC);
		CREATE INDEX IF NOT EXISTS idx_flagged_groups_scan_time ON flagged_groups (last_scanned ASC);
		
		-- Group tracking indexes
		CREATE INDEX IF NOT EXISTS idx_group_member_trackings_check 
		ON group_member_trackings (is_flagged, last_checked ASC) 
		WHERE is_flagged = false;
		
		CREATE INDEX IF NOT EXISTS idx_group_member_trackings_group_id
		ON group_member_trackings (group_id);
		
		CREATE INDEX IF NOT EXISTS idx_group_member_trackings_cleanup
		ON group_member_trackings (last_appended)
		WHERE is_flagged = false;
		
		-- Group review sorting indexes
		CREATE INDEX IF NOT EXISTS idx_flagged_groups_confidence_viewed 
		ON flagged_groups (confidence DESC, last_viewed ASC);
		CREATE INDEX IF NOT EXISTS idx_confirmed_groups_confidence_viewed 
		ON confirmed_groups (confidence DESC, last_viewed ASC);
		
		CREATE INDEX IF NOT EXISTS idx_flagged_groups_reputation_viewed 
		ON flagged_groups (reputation ASC, last_viewed ASC);
		CREATE INDEX IF NOT EXISTS idx_confirmed_groups_reputation_viewed 
		ON confirmed_groups (reputation ASC, last_viewed ASC);

		-- User review sorting indexes
		CREATE INDEX IF NOT EXISTS idx_flagged_users_confidence_viewed 
		ON flagged_users (confidence DESC, last_viewed ASC);
		CREATE INDEX IF NOT EXISTS idx_confirmed_users_confidence_viewed 
		ON confirmed_users (confidence DESC, last_viewed ASC);
		
		CREATE INDEX IF NOT EXISTS idx_flagged_users_reputation_viewed 
		ON flagged_users (reputation ASC, last_viewed ASC);
		CREATE INDEX IF NOT EXISTS idx_confirmed_users_reputation_viewed 
		ON confirmed_users (reputation ASC, last_viewed ASC);
		
		CREATE INDEX IF NOT EXISTS idx_flagged_users_updated_viewed 
		ON flagged_users (last_updated ASC, last_viewed ASC);
		CREATE INDEX IF NOT EXISTS idx_confirmed_users_updated_viewed 
		ON confirmed_users (last_updated ASC, last_viewed ASC);

		-- User status indexes
		CREATE INDEX IF NOT EXISTS idx_cleared_users_purged_at ON cleared_users (cleared_at);
		CREATE INDEX IF NOT EXISTS idx_flagged_users_last_purge_check ON flagged_users (last_purge_check);
		CREATE INDEX IF NOT EXISTS idx_confirmed_users_last_purge_check ON confirmed_users (last_purge_check);
		
		-- Group status indexes
		CREATE INDEX IF NOT EXISTS idx_cleared_groups_purged_at ON cleared_groups (cleared_at);
		CREATE INDEX IF NOT EXISTS idx_flagged_groups_last_purge_check ON flagged_groups (last_purge_check);
		CREATE INDEX IF NOT EXISTS idx_confirmed_groups_last_purge_check ON confirmed_groups (last_purge_check);
		
		-- Statistics indexes
		CREATE INDEX IF NOT EXISTS idx_hourly_stats_timestamp ON hourly_stats (timestamp DESC);
	`).Exec(context.Background())
	if err != nil {
		c.logger.Error("Failed to create indexes", zap.Error(err))
		return err
	}
	c.logger.Info("Indexes created or already exist")

	return nil
}

// setupTimescaleDB initializes the TimescaleDB extension and creates hypertables.
func (c *Client) setupTimescaleDB() error {
	// Check if TimescaleDB extension exists
	var exists bool
	err := c.db.NewRaw(`
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
		_, err = c.db.NewRaw(`CREATE EXTENSION IF NOT EXISTS timescaledb`).
			Exec(context.Background())
		if err != nil {
			return fmt.Errorf("failed to create TimescaleDB extension: %w", err)
		}
		c.logger.Info("TimescaleDB extension created")
	} else {
		c.logger.Info("TimescaleDB extension already exists")
	}

	// Create hypertables
	_, err = c.db.NewRaw(`
		SELECT create_hypertable('user_activity_logs', 'activity_timestamp', 
			chunk_time_interval => INTERVAL '1 day',
			if_not_exists => TRUE
		);

		SELECT create_hypertable('appeal_timelines', 'timestamp', 
			chunk_time_interval => INTERVAL '1 day',
			if_not_exists => TRUE
		);
	`).Exec(context.Background())
	if err != nil {
		return fmt.Errorf("failed to create hypertable: %w", err)
	}

	return nil
}

// setupSequences creates and initializes all required sequences.
func (c *Client) setupSequences() error {
	_, err := c.db.NewRaw(`
		-- Create sequence for generating unique log identifiers
		CREATE SEQUENCE IF NOT EXISTS user_activity_logs_sequence;
	`).Exec(context.Background())
	if err != nil {
		return fmt.Errorf("failed to setup sequences: %w", err)
	}

	return nil
}

// Close gracefully shuts down the database connection.
func (c *Client) Close() error {
	err := c.db.Close()
	if err != nil {
		c.logger.Error("Failed to close database connection", zap.Error(err))
		return err
	}
	c.logger.Info("Database connection closed")
	return nil
}

// Users returns the repository for user-related operations.
func (c *Client) Users() *models.UserModel {
	return c.users
}

// Groups returns the repository for group-related operations.
func (c *Client) Groups() *models.GroupModel {
	return c.groups
}

// Stats returns the repository for statistics operations.
func (c *Client) Stats() *models.StatsModel {
	return c.stats
}

// Settings returns the repository for user and guild settings.
func (c *Client) Settings() *models.SettingModel {
	return c.settings
}

// Tracking returns the repository for tracking user and group relationships.
func (c *Client) Tracking() *models.TrackingModel {
	return c.tracking
}

// UserActivity returns the repository for logging user actions.
func (c *Client) UserActivity() *models.ActivityModel {
	return c.userActivity
}

// Appeals returns the repository for appeal-related operations.
func (c *Client) Appeals() *models.AppealModel {
	return c.appeals
}

package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/disgoorg/snowflake/v2"
	"github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/rotector/rotector/internal/common/config"
	"github.com/rotector/rotector/internal/common/statistics"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
	"go.uber.org/zap"
)

const (
	// SortByRandom orders users randomly to ensure even distribution of reviews.
	SortByRandom = "random"
	// SortByConfidence orders users by their confidence score from highest to lowest.
	SortByConfidence = "confidence"
	// SortByLastUpdated orders users by their last update time from oldest to newest.
	SortByLastUpdated = "last_updated"
)

// ErrInvalidSortBy indicates that the provided sort method is not supported.
var ErrInvalidSortBy = errors.New("invalid sortBy value")

// FlaggedGroup stores information about a group that needs review.
// The confidence score helps prioritize which groups to review first.
type FlaggedGroup struct {
	ID           uint64    `bun:",pk"`
	Name         string    `bun:",notnull"`
	Description  string    `bun:",notnull"`
	Owner        uint64    `bun:",notnull"`
	Reason       string    `bun:",notnull"`
	Confidence   float64   `bun:",notnull"`
	LastUpdated  time.Time `bun:",notnull"`
	ThumbnailURL string
}

// ConfirmedGroup stores information about a group that has been reviewed and confirmed.
// The last_scanned field helps track when to re-check the group's members.
type ConfirmedGroup struct {
	ID          uint64 `bun:",pk"`
	Name        string `bun:",notnull"`
	Description string `bun:",notnull"`
	Owner       uint64 `bun:",notnull"`
	LastScanned time.Time
}

// User combines all the information needed to review a user.
// This base structure is embedded in other user types (Flagged, Confirmed).
type User struct {
	ID             uint64                 `bun:",pk"        json:"id"`
	Name           string                 `bun:",notnull"   json:"name"`
	DisplayName    string                 `bun:",notnull"   json:"displayName"`
	Description    string                 `bun:",notnull"   json:"description"`
	CreatedAt      time.Time              `bun:",notnull"   json:"createdAt"`
	Reason         string                 `bun:",notnull"   json:"reason"`
	Groups         []types.UserGroupRoles `bun:"type:jsonb" json:"groups"`
	Outfits        []types.Outfit         `bun:"type:jsonb" json:"outfits"`
	Friends        []types.Friend         `bun:"type:jsonb" json:"friends"`
	FlaggedContent []string               `bun:"type:jsonb" json:"flaggedContent"`
	FlaggedGroups  []uint64               `bun:"type:jsonb" json:"flaggedGroups"`
	Confidence     float64                `bun:",notnull"   json:"confidence"`
	LastScanned    time.Time              `bun:",notnull"   json:"lastScanned"`
	LastUpdated    time.Time              `bun:",notnull"   json:"lastUpdated"`
	LastViewed     time.Time              `bun:",notnull"   json:"lastViewed"`
	LastPurgeCheck time.Time              `bun:",notnull"   json:"lastPurgeCheck"`
	ThumbnailURL   string                 `bun:",notnull"   json:"thumbnailUrl"`
}

// FlaggedUser extends User to track users that need review.
// The base User structure contains all the fields needed for review.
type FlaggedUser struct {
	User
}

// ConfirmedUser extends User to track users that have been reviewed and confirmed.
// The VerifiedAt field shows when the user was confirmed by a moderator.
type ConfirmedUser struct {
	User
	VerifiedAt time.Time `bun:",notnull" json:"verifiedAt"`
}

// ClearedUser extends User to track users that were cleared during review.
// The ClearedAt field shows when the user was cleared by a moderator.
type ClearedUser struct {
	User
	ClearedAt time.Time `bun:",notnull" json:"clearedAt"`
}

// BannedUser extends User to track users that were banned and removed.
// The PurgedAt field shows when the user was removed from the system.
type BannedUser struct {
	User
	PurgedAt time.Time `bun:",notnull" json:"purgedAt"`
}

// DailyStatistics tracks daily counts of activities and purges.
// The date field serves as the primary key for grouping statistics.
type DailyStatistics struct {
	Date               time.Time `bun:",pk"`
	UsersConfirmed     int64     `bun:",notnull"`
	UsersFlagged       int64     `bun:",notnull"`
	UsersCleared       int64     `bun:",notnull"`
	BannedUsersPurged  int64     `bun:",notnull"`
	FlaggedUsersPurged int64     `bun:",notnull"`
	ClearedUsersPurged int64     `bun:",notnull"`
}

// UserSetting stores user-specific preferences.
type UserSetting struct {
	UserID       uint64 `bun:",pk"`
	StreamerMode bool   `bun:",notnull"`
	DefaultSort  string `bun:",notnull"`
}

// GuildSetting stores server-wide configuration options.
type GuildSetting struct {
	GuildID          uint64   `bun:",pk"`
	WhitelistedRoles []uint64 `bun:"type:bigint[]"`
}

// GroupMemberTracking monitors confirmed users within groups.
// The LastAppended field helps determine when to purge old tracking data.
type GroupMemberTracking struct {
	GroupID        uint64    `bun:",pk"`
	ConfirmedUsers []uint64  `bun:"type:bigint[]"`
	LastAppended   time.Time `bun:",notnull"`
}

// HasAnyRole checks if any of the provided role IDs match the whitelisted roles.
// Returns true if there is at least one match, false otherwise.
func (s *GuildSetting) HasAnyRole(roleIDs []snowflake.ID) bool {
	for _, roleID := range roleIDs {
		if slices.Contains(s.WhitelistedRoles, uint64(roleID)) {
			return true
		}
	}
	return false
}

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
// It initializes all repositories and creates necessary tables and indexes.
func NewConnection(config *config.Config, stats *statistics.Client, logger *zap.Logger) (*Database, error) {
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
		users:        NewUserRepository(db, stats, tracking, logger),
		groups:       NewGroupRepository(db, logger),
		stats:        NewStatsRepository(db, stats, logger),
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
		(*DailyStatistics)(nil),
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
		CREATE INDEX IF NOT EXISTS idx_user_activity_logs_user_id ON user_activity_logs (user_id);
		CREATE INDEX IF NOT EXISTS idx_user_activity_logs_reviewer_id ON user_activity_logs (reviewer_id);
		CREATE INDEX IF NOT EXISTS idx_user_activity_logs_activity_timestamp ON user_activity_logs (activity_timestamp);

		CREATE INDEX IF NOT EXISTS idx_group_member_trackings_last_appended ON group_member_trackings (last_appended);
		CREATE INDEX IF NOT EXISTS idx_group_member_trackings_group_id_array_length 
		ON group_member_trackings USING btree (group_id, array_length(confirmed_users, 1));

		CREATE INDEX IF NOT EXISTS idx_cleared_users_cleared_at ON cleared_users (cleared_at);
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
// It logs any errors that occur during shutdown.
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

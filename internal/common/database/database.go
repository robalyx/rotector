package database

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/disgoorg/snowflake/v2"
	"github.com/go-pg/pg/v10"
	"github.com/go-pg/pg/v10/orm"
	"github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/rotector/rotector/internal/common/config"
	"github.com/rotector/rotector/internal/common/statistics"
	"go.uber.org/zap"
)

const (
	SortByRandom      = "random"
	SortByConfidence  = "confidence"
	SortByLastUpdated = "last_updated"
)

var ErrInvalidSortBy = errors.New("invalid sortBy value")

// ConfirmedGroup represents a group that is considered flagged.
type ConfirmedGroup struct {
	ID          uint64    `pg:"id,pk"`
	Name        string    `pg:"name"`
	Description string    `pg:"description"`
	Owner       uint64    `pg:"owner"`
	LastScanned time.Time `pg:"last_scanned"`
}

// User represents a user in the database.
type User struct {
	ID             uint64                 `json:"id"             pg:"id,pk"`
	Name           string                 `json:"name"           pg:"name"`
	DisplayName    string                 `json:"displayName"    pg:"display_name"`
	Description    string                 `json:"description"    pg:"description"`
	CreatedAt      time.Time              `json:"createdAt"      pg:"created_at"`
	Reason         string                 `json:"reason"         pg:"reason"`
	Groups         []types.UserGroupRoles `json:"groups"         pg:"groups"`
	Outfits        []types.Outfit         `json:"outfits"        pg:"outfits"`
	Friends        []types.Friend         `json:"friends"        pg:"friends"`
	FlaggedContent []string               `json:"flaggedContent" pg:"flagged_content"`
	FlaggedGroups  []uint64               `json:"flaggedGroups"  pg:"flagged_groups"`
	Confidence     float64                `json:"confidence"     pg:"confidence"`
	LastScanned    time.Time              `json:"lastScanned"    pg:"last_scanned"`
	LastUpdated    time.Time              `json:"lastUpdated"    pg:"last_updated"`
	LastReviewed   time.Time              `json:"lastReviewed"   pg:"last_reviewed"`
	LastPurgeCheck time.Time              `json:"lastPurgeCheck" pg:"last_purge_check"`
	ThumbnailURL   string                 `json:"thumbnailUrl"   pg:"thumbnail_url"`
}

// FlaggedUser represents a user that is flagged for review.
type FlaggedUser struct {
	User
}

// ConfirmedUser represents a user that is considered confirmed.
type ConfirmedUser struct {
	User
	VerifiedAt time.Time `json:"verifiedAt" pg:"verified_at"`
}

// DailyStatistics represents daily statistics.
type DailyStatistics struct {
	Date         time.Time `pg:"date,pk"`
	UsersBanned  int64     `pg:"users_banned"`
	UsersCleared int64     `pg:"users_cleared"`
	UsersFlagged int64     `pg:"users_flagged"`
	UsersPurged  int64     `pg:"users_purged"`
}

// UserSetting represents user-specific settings.
type UserSetting struct {
	UserID       uint64 `pg:"user_id,pk"`
	StreamerMode bool   `pg:"streamer_mode"`
	DefaultSort  string `pg:"default_sort"`
}

// GuildSetting represents guild-wide settings.
type GuildSetting struct {
	GuildID          uint64   `pg:"guild_id,pk"`
	WhitelistedRoles []uint64 `pg:"whitelisted_roles,array"`
}

// GroupMemberTracking represents the tracking of confirmed users in a group.
type GroupMemberTracking struct {
	GroupID        uint64    `pg:"group_id,pk"`
	ConfirmedUsers []uint64  `pg:"confirmed_users,array"`
	LastAppended   time.Time `pg:"last_appended,notnull"`
}

// UserAffiliateTracking represents the tracking of confirmed users in a user's friend list.
type UserAffiliateTracking struct {
	UserID         uint64    `pg:"user_id,pk"`
	ConfirmedUsers []uint64  `pg:"confirmed_users,array"`
	LastAppended   time.Time `pg:"last_appended,notnull"`
}

func (s *GuildSetting) HasAnyRole(roleIDs []snowflake.ID) bool {
	for _, roleID := range roleIDs {
		if slices.Contains(s.WhitelistedRoles, uint64(roleID)) {
			return true
		}
	}
	return false
}

// Database represents the database connection and operations.
type Database struct {
	db       *pg.DB
	logger   *zap.Logger
	users    *UserRepository
	groups   *GroupRepository
	stats    *StatsRepository
	settings *SettingRepository
}

// NewConnection establishes a new database connection and returns a Database instance.
func NewConnection(config *config.Config, stats *statistics.Statistics, logger *zap.Logger) (*Database, error) {
	// Initialize database connection
	db := pg.Connect(&pg.Options{
		Addr:     fmt.Sprintf("%s:%d", config.PostgreSQL.Host, config.PostgreSQL.Port),
		User:     config.PostgreSQL.User,
		Password: config.PostgreSQL.Password,
		Database: config.PostgreSQL.DBName,
	})

	// Create database instance
	tracking := NewTrackingRepository(db, logger)
	database := &Database{
		db:       db,
		logger:   logger,
		users:    NewUserRepository(db, stats, tracking, logger),
		groups:   NewGroupRepository(db, logger),
		stats:    NewStatsRepository(db, stats.Client, logger),
		settings: NewSettingRepository(db, logger),
	}

	if err := database.createSchema(); err != nil {
		return nil, fmt.Errorf("failed to create schema: %w", err)
	}

	logger.Info("Database connection established and setup completed")
	return database, nil
}

// createSchema creates the necessary database tables if they don't exist.
func (d *Database) createSchema() error {
	models := []interface{}{
		(*ConfirmedGroup)(nil),
		(*FlaggedUser)(nil),
		(*ConfirmedUser)(nil),
		(*DailyStatistics)(nil),
		(*UserSetting)(nil),
		(*GuildSetting)(nil),
		(*GroupMemberTracking)(nil),
		(*UserAffiliateTracking)(nil),
	}

	for _, model := range models {
		err := d.db.Model(model).CreateTable(&orm.CreateTableOptions{
			IfNotExists: true,
		})
		if err != nil {
			d.logger.Error("Failed to create table", zap.Error(err), zap.String("model", fmt.Sprintf("%T", model)))
			return err
		}
		d.logger.Info("Table created or already exists", zap.String("model", fmt.Sprintf("%T", model)))
	}

	return nil
}

// Close closes the database connection.
func (d *Database) Close() error {
	err := d.db.Close()
	if err != nil {
		d.logger.Error("Failed to close database connection", zap.Error(err))
		return err
	}
	d.logger.Info("Database connection closed")
	return nil
}

// Users returns the UserRepository.
func (d *Database) Users() *UserRepository {
	return d.users
}

// Groups returns the GroupRepository.
func (d *Database) Groups() *GroupRepository {
	return d.groups
}

// Stats returns the StatsRepository.
func (d *Database) Stats() *StatsRepository {
	return d.stats
}

// Settings returns the SettingRepository.
func (d *Database) Settings() *SettingRepository {
	return d.settings
}

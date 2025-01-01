package database

import (
	"database/sql"
	"fmt"
	"io"
	"time"

	"github.com/bytedance/sonic"
	"github.com/rotector/rotector/internal/common/config"
	"github.com/rotector/rotector/internal/common/storage/database/models"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
	"github.com/uptrace/bun/extra/bunjson"
	"go.uber.org/zap"
)

// PartitionCount is the number of partitions for user and group tables.
const PartitionCount = 8

// sonicProvider is a JSON provider that uses Sonic for encoding and decoding.
type sonicProvider struct{}

func (sonicProvider) Marshal(v interface{}) ([]byte, error) {
	return sonic.Marshal(v)
}

func (sonicProvider) Unmarshal(data []byte, v interface{}) error {
	return sonic.Unmarshal(data, v)
}

func (sonicProvider) NewEncoder(w io.Writer) bunjson.Encoder {
	return sonic.ConfigDefault.NewEncoder(w)
}

func (sonicProvider) NewDecoder(r io.Reader) bunjson.Decoder {
	return sonic.ConfigDefault.NewDecoder(r)
}

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

	// Set Sonic as the JSON provider
	bunjson.SetProvider(sonicProvider{})

	// Create Bun db instance
	db := bun.NewDB(sqldb, pgdialect.New())
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

	logger.Info("Database connection established")
	return client, nil
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

// DB returns the underlying bun.DB instance.
func (c *Client) DB() *bun.DB {
	return c.db
}

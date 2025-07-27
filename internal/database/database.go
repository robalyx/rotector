package database

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"time"

	"github.com/bytedance/sonic"
	"github.com/robalyx/rotector/internal/database/migrations"
	"github.com/robalyx/rotector/internal/setup/config"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
	"github.com/uptrace/bun/extra/bunjson"
	"github.com/uptrace/bun/migrate"
	"go.uber.org/zap"
)

// PartitionCount is the number of partitions for user and group tables.
const PartitionCount = 8

// sonicProvider is a JSON provider that uses Sonic for encoding and decoding.
type sonicProvider struct{}

func (sonicProvider) Marshal(v any) ([]byte, error) {
	return sonic.Marshal(v)
}

func (sonicProvider) Unmarshal(data []byte, v any) error {
	return sonic.Unmarshal(data, v)
}

func (sonicProvider) NewEncoder(w io.Writer) bunjson.Encoder {
	return sonic.ConfigDefault.NewEncoder(w)
}

func (sonicProvider) NewDecoder(r io.Reader) bunjson.Decoder {
	return sonic.ConfigDefault.NewDecoder(r)
}

// Client defines the methods that a database client must implement.
type Client interface {
	// Model returns the repository containing all model operations.
	Model() *Repository
	// Services returns the service containing all service operations.
	Service() *Service
	// Close gracefully shuts down the database connection.
	Close() error
	// DB returns the underlying bun.DB instance.
	DB() *bun.DB
}

// clientImpl represents the concrete implementation of the database client.
type clientImpl struct {
	db      *bun.DB
	logger  *zap.Logger
	repo    *Repository
	service *Service
}

// NewConnection establishes a new database connection and returns a Client instance.
func NewConnection(
	ctx context.Context, config *config.PostgreSQL, logger *zap.Logger, autoMigrate bool,
) (Client, error) {
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

	// Add query hook for monitoring
	db.AddQueryHook(NewHook(logger))

	// Run migrations if requested
	if autoMigrate {
		migrator := migrate.NewMigrator(db, migrations.Migrations)
		if err := migrator.Init(ctx); err != nil {
			return nil, fmt.Errorf("failed to initialize migrations: %w", err)
		}

		group, err := migrator.Migrate(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to run migrations: %w", err)
		}

		if !group.IsZero() {
			logger.Info("Automatically ran migrations", zap.String("group", group.String()))
		}
	}

	// Create client instance
	repo := NewRepository(db, logger)
	service := NewService(db, repo, logger)

	client := &clientImpl{
		db:      db,
		logger:  logger,
		repo:    repo,
		service: service,
	}

	logger.Info("Database connection established")

	return client, nil
}

// Close gracefully shuts down the database connection.
func (c *clientImpl) Close() error {
	err := c.db.Close()
	if err != nil {
		c.logger.Error("Failed to close database connection", zap.Error(err))
		return err
	}

	c.logger.Info("Database connection closed")

	return nil
}

// Model returns the repository containing all model operations.
func (c *clientImpl) Model() *Repository {
	return c.repo
}

// Service returns the service containing all service operations.
func (c *clientImpl) Service() *Service {
	return c.service
}

// DB returns the underlying bun.DB instance.
func (c *clientImpl) DB() *bun.DB {
	return c.db
}

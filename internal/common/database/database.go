package database

import (
	"errors"
	"fmt"
	"time"

	"github.com/go-pg/pg/v10"
	"github.com/go-pg/pg/v10/orm"
	"github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/rotector/rotector/internal/common/config"
	"go.uber.org/zap"
)

const (
	SortByRandom      = "random"
	SortByConfidence  = "confidence"
	SortByLastUpdated = "last_updated"
)

var ErrInvalidSortBy = errors.New("invalid sortBy value")

// FlaggedGroup represents a group that is considered flagged.
type FlaggedGroup struct {
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
	ThumbnailURL   string                 `json:"thumbnailUrl"   pg:"thumbnail_url"`
}

// PendingUser represents a user that is pending review.
type PendingUser struct {
	User
}

// FlaggedUser represents a user that is considered flagged.
type FlaggedUser struct {
	User
	VerifiedAt time.Time `json:"verifiedAt" pg:"verified_at"`
}

// Database represents the database connection and operations.
type Database struct {
	db     *pg.DB
	logger *zap.Logger
	users  *UserRepository
	groups *GroupRepository
}

// NewConnection establishes a new database connection and returns a Database instance.
func NewConnection(dbConfig config.PostgreSQL, logger *zap.Logger) (*Database, error) {
	db := pg.Connect(&pg.Options{
		Addr:     fmt.Sprintf("%s:%d", dbConfig.Host, dbConfig.Port),
		User:     dbConfig.User,
		Password: dbConfig.Password,
		Database: dbConfig.DBName,
	})

	database := &Database{
		db:     db,
		logger: logger,
		users:  NewUserRepository(db, logger),
		groups: NewGroupRepository(db, logger),
	}

	err := database.createSchema()
	if err != nil {
		return nil, fmt.Errorf("failed to create schema: %w", err)
	}

	logger.Info("Database connection established and schema created")
	return database, nil
}

// createSchema creates the necessary database tables if they don't exist.
func (d *Database) createSchema() error {
	models := []interface{}{
		(*FlaggedGroup)(nil),
		(*PendingUser)(nil),
		(*FlaggedUser)(nil),
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

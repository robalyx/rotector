package database

import (
	"errors"

	"github.com/go-pg/pg/v10"
	"go.uber.org/zap"
)

// GroupRepository handles group-related database operations.
type GroupRepository struct {
	db     *pg.DB
	logger *zap.Logger
}

// NewGroupRepository creates a new GroupRepository instance.
func NewGroupRepository(db *pg.DB, logger *zap.Logger) *GroupRepository {
	return &GroupRepository{
		db:     db,
		logger: logger,
	}
}

// GetNextConfirmedGroup retrieves the next confirmed group to be processed.
func (r *GroupRepository) GetNextConfirmedGroup() (*ConfirmedGroup, error) {
	var group ConfirmedGroup
	_, err := r.db.QueryOne(&group, `
		UPDATE confirmed_groups
		SET last_scanned = NOW() 
		WHERE id = (
			SELECT id 
			FROM confirmed_groups
			WHERE last_scanned IS NULL OR last_scanned < NOW() - INTERVAL '1 day'
			ORDER BY last_scanned ASC NULLS FIRST
			LIMIT 1
		)
		RETURNING *
	`)
	if err != nil {
		r.logger.Error("Failed to get next confirmed group", zap.Error(err))
		return nil, err
	}
	r.logger.Info("Retrieved next confirmed group", zap.Uint64("groupID", group.ID), zap.Time("lastScanned", group.LastScanned))
	return &group, nil
}

// CheckConfirmedGroups checks if any of the provided group IDs are confirmed.
func (r *GroupRepository) CheckConfirmedGroups(groupIDs []uint64) ([]uint64, error) {
	var confirmedGroupIDs []uint64
	_, err := r.db.Query(&confirmedGroupIDs, `
		SELECT id
		FROM confirmed_groups
		WHERE id = ANY(?)
	`, pg.Array(groupIDs))

	if errors.Is(err, pg.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}

	return confirmedGroupIDs, nil
}

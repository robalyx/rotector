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

// GetNextFlaggedGroup retrieves the next flagged group to be processed.
func (r *GroupRepository) GetNextFlaggedGroup() (*FlaggedGroup, error) {
	var group FlaggedGroup
	_, err := r.db.QueryOne(&group, `
		UPDATE flagged_groups
		SET last_scanned = NOW() 
		WHERE id = (
			SELECT id 
			FROM flagged_groups
			WHERE last_scanned IS NULL OR last_scanned < NOW() - INTERVAL '1 day'
			ORDER BY last_scanned ASC NULLS FIRST
			LIMIT 1
		)
		RETURNING *
	`)
	if err != nil {
		r.logger.Error("Failed to get next flagged group", zap.Error(err))
		return nil, err
	}
	r.logger.Info("Retrieved next flagged group", zap.Uint64("groupID", group.ID), zap.Time("lastScanned", group.LastScanned))
	return &group, nil
}

// CheckFlaggedGroups checks if any of the provided group IDs are flagged.
func (r *GroupRepository) CheckFlaggedGroups(groupIDs []uint64) ([]uint64, error) {
	var flaggedGroupIDs []uint64
	_, err := r.db.Query(&flaggedGroupIDs, `
		SELECT id
		FROM flagged_groups
		WHERE id = ANY(?)
	`, pg.Array(groupIDs))

	if errors.Is(err, pg.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}

	return flaggedGroupIDs, nil
}

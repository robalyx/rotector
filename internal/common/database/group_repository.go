package database

import (
	"time"

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

	err := r.db.RunInTransaction(r.db.Context(), func(tx *pg.Tx) error {
		err := tx.Model(&group).
			Where("last_scanned IS NULL OR last_scanned < NOW() - INTERVAL '1 day'").
			Order("last_scanned ASC NULLS FIRST").
			Limit(1).
			For("UPDATE SKIP LOCKED").
			Select()
		if err != nil {
			r.logger.Error("Failed to get next confirmed group", zap.Error(err))
			return err
		}

		_, err = tx.Model(&group).
			Set("last_scanned = ?", time.Now()).
			Where("id = ?", group.ID).
			Update()
		if err != nil {
			r.logger.Error("Failed to update last_scanned", zap.Error(err))
			return err
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	r.logger.Info("Retrieved and updated next confirmed group",
		zap.Uint64("groupID", group.ID),
		zap.Time("lastScanned", group.LastScanned))
	return &group, nil
}

// CheckConfirmedGroups checks if any of the provided group IDs are confirmed.
func (r *GroupRepository) CheckConfirmedGroups(groupIDs []uint64) ([]uint64, error) {
	var confirmedGroupIDs []uint64
	err := r.db.Model((*ConfirmedGroup)(nil)).
		Column("id").
		Where("id IN (?)", pg.In(groupIDs)).
		Select(&confirmedGroupIDs)
	if err != nil {
		r.logger.Error("Failed to check confirmed groups", zap.Error(err), zap.Uint64s("groupIDs", groupIDs))
		return nil, err
	}

	return confirmedGroupIDs, nil
}

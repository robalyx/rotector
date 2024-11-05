package database

import (
	"time"

	"github.com/go-pg/pg/v10"
	"go.uber.org/zap"
)

// GroupRepository handles database operations for group records.
type GroupRepository struct {
	db     *pg.DB
	logger *zap.Logger
}

// NewGroupRepository creates a GroupRepository with database access for
// storing and retrieving group information.
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

// CheckConfirmedGroups finds which groups from a list of IDs exist in confirmed_groups.
// Returns a slice of confirmed group IDs.
func (r *GroupRepository) CheckConfirmedGroups(groupIDs []uint64) ([]uint64, error) {
	var confirmedGroupIDs []uint64
	err := r.db.Model((*ConfirmedGroup)(nil)).
		Column("id").
		Where("id IN (?)", pg.In(groupIDs)).
		Select(&confirmedGroupIDs)
	if err != nil {
		r.logger.Error("Failed to check confirmed groups", zap.Error(err))
		return nil, err
	}

	r.logger.Debug("Checked confirmed groups",
		zap.Int("total", len(groupIDs)),
		zap.Int("confirmed", len(confirmedGroupIDs)))

	return confirmedGroupIDs, nil
}

// SaveFlaggedGroups adds or updates groups in the flagged_groups table.
// For each group, it updates all fields if the group already exists,
// or inserts a new record if they don't.
func (r *GroupRepository) SaveFlaggedGroups(flaggedGroups []*FlaggedGroup) {
	r.logger.Info("Saving flagged groups", zap.Int("count", len(flaggedGroups)))

	for _, flaggedGroup := range flaggedGroups {
		_, err := r.db.Model(flaggedGroup).
			OnConflict("(id) DO UPDATE").
			Set("name = EXCLUDED.name").
			Set("description = EXCLUDED.description").
			Set("owner = EXCLUDED.owner").
			Set("reason = EXCLUDED.reason").
			Set("confidence = EXCLUDED.confidence").
			Set("last_updated = EXCLUDED.last_updated").
			Set("thumbnail_url = EXCLUDED.thumbnail_url").
			Insert()
		if err != nil {
			r.logger.Error("Error saving flagged group",
				zap.Uint64("groupID", flaggedGroup.ID),
				zap.String("name", flaggedGroup.Name),
				zap.String("reason", flaggedGroup.Reason),
				zap.Float64("confidence", flaggedGroup.Confidence),
				zap.Error(err))
			continue
		}

		r.logger.Info("Saved flagged group",
			zap.Uint64("groupID", flaggedGroup.ID),
			zap.String("name", flaggedGroup.Name),
			zap.String("reason", flaggedGroup.Reason),
			zap.Float64("confidence", flaggedGroup.Confidence),
			zap.Time("last_updated", time.Now()),
			zap.String("thumbnail_url", flaggedGroup.ThumbnailURL))
	}

	r.logger.Info("Finished saving flagged groups")
}

// ConfirmGroup moves a group from flagged_groups to confirmed_groups.
// This happens when a moderator confirms that a group is inappropriate.
func (r *GroupRepository) ConfirmGroup(group *FlaggedGroup) error {
	return r.db.RunInTransaction(r.db.Context(), func(tx *pg.Tx) error {
		confirmedGroup := &ConfirmedGroup{
			ID:          group.ID,
			Name:        group.Name,
			Description: group.Description,
			Owner:       group.Owner,
		}

		_, err := tx.Model(confirmedGroup).
			OnConflict("(id) DO UPDATE").
			Set("name = EXCLUDED.name").
			Set("description = EXCLUDED.description").
			Set("owner = EXCLUDED.owner").
			Insert()
		if err != nil {
			r.logger.Error("Failed to insert or update group in confirmed_groups", zap.Error(err), zap.Uint64("groupID", group.ID))
			return err
		}

		_, err = tx.Model((*FlaggedGroup)(nil)).Where("id = ?", group.ID).Delete()
		if err != nil {
			r.logger.Error("Failed to delete group from flagged_groups", zap.Error(err), zap.Uint64("groupID", group.ID))
			return err
		}

		return nil
	})
}

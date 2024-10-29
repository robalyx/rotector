package database

import (
	"time"

	"github.com/go-pg/pg/v10"
	"go.uber.org/zap"
)

// TrackingRepository handles operations for group member and user network tracking.
type TrackingRepository struct {
	db     *pg.DB
	logger *zap.Logger
}

// NewTrackingRepository creates a new TrackingRepository instance.
func NewTrackingRepository(db *pg.DB, logger *zap.Logger) *TrackingRepository {
	return &TrackingRepository{
		db:     db,
		logger: logger,
	}
}

// AddUserToGroupTracking adds a confirmed user to the group member tracking.
func (r *TrackingRepository) AddUserToGroupTracking(groupID, userID uint64) error {
	return r.db.RunInTransaction(r.db.Context(), func(tx *pg.Tx) error {
		_, err := tx.Exec(`
			INSERT INTO group_member_trackings (group_id, confirmed_users, last_appended)
			VALUES (?, ARRAY[?], NOW())
			ON CONFLICT (group_id) DO UPDATE
			SET confirmed_users = array_append(group_member_trackings.confirmed_users, EXCLUDED.confirmed_users[1]),
				last_appended = NOW()
			WHERE NOT EXCLUDED.confirmed_users[1] = ANY(group_member_trackings.confirmed_users)
		`, groupID, userID)
		if err != nil {
			r.logger.Error("Failed to add user to group tracking", zap.Error(err), zap.Uint64("groupID", groupID), zap.Uint64("userID", userID))
			return err
		}

		return nil
	})
}

// AddUserToNetworkTracking adds a confirmed user to the user network tracking.
func (r *TrackingRepository) AddUserToNetworkTracking(userID, networkUserID uint64) error {
	return r.db.RunInTransaction(r.db.Context(), func(tx *pg.Tx) error {
		_, err := tx.Exec(`
			INSERT INTO user_network_trackings (user_id, confirmed_users, last_appended)
			VALUES (?, ARRAY[?], NOW())
			ON CONFLICT (user_id) DO UPDATE
			SET confirmed_users = array_append(user_network_trackings.confirmed_users, EXCLUDED.confirmed_users[1]),
				last_appended = NOW()
			WHERE NOT EXCLUDED.confirmed_users[1] = ANY(user_network_trackings.confirmed_users)
		`, userID, networkUserID)
		if err != nil {
			r.logger.Error("Failed to add user to network tracking", zap.Error(err), zap.Uint64("userID", userID), zap.Uint64("networkUserID", networkUserID))
			return err
		}

		return nil
	})
}

// PurgeOldGroupMemberTrackings removes old entries from group_member_trackings.
func (r *TrackingRepository) PurgeOldGroupMemberTrackings(cutoffDate time.Time, batchSize int) (int, error) {
	var affected int
	err := r.db.RunInTransaction(r.db.Context(), func(tx *pg.Tx) error {
		// Select the IDs to delete
		var groupIDs []uint64
		err := tx.Model((*GroupMemberTracking)(nil)).
			Column("group_id").
			Where("last_appended < ?", cutoffDate).
			Order("last_appended ASC").
			Limit(batchSize).
			For("UPDATE SKIP LOCKED").
			Select(&groupIDs)
		if err != nil {
			return err
		}

		if len(groupIDs) == 0 {
			return nil
		}

		// Delete the selected records
		result, err := tx.Model((*GroupMemberTracking)(nil)).
			Where("group_id IN (?)", pg.In(groupIDs)).
			Delete()
		if err != nil {
			return err
		}

		affected = result.RowsAffected()
		return nil
	})

	return affected, err
}

// PurgeOldUserNetworkTrackings removes old entries from user_network_trackings.
func (r *TrackingRepository) PurgeOldUserNetworkTrackings(cutoffDate time.Time, batchSize int) (int, error) {
	var affected int
	err := r.db.RunInTransaction(r.db.Context(), func(tx *pg.Tx) error {
		// Select the IDs to delete
		var userIDs []uint64
		err := tx.Model((*UserNetworkTracking)(nil)).
			Column("user_id").
			Where("last_appended < ?", cutoffDate).
			Order("last_appended ASC").
			Limit(batchSize).
			For("UPDATE SKIP LOCKED").
			Select(&userIDs)
		if err != nil {
			return err
		}

		if len(userIDs) == 0 {
			return nil
		}

		// Delete the selected records
		result, err := tx.Model((*UserNetworkTracking)(nil)).
			Where("user_id IN (?)", pg.In(userIDs)).
			Delete()
		if err != nil {
			return err
		}

		affected = result.RowsAffected()
		return nil
	})

	return affected, err
}

// GetAndRemoveQualifiedGroupTrackings retrieves and removes groups that have sufficient confirmed users.
func (r *TrackingRepository) GetAndRemoveQualifiedGroupTrackings(minUsers int) (map[uint64]int, error) {
	groupsMap := make(map[uint64]int)

	err := r.db.RunInTransaction(r.db.Context(), func(tx *pg.Tx) error {
		var groups []struct {
			GroupID uint64 `pg:"group_id"`
			Count   int    `pg:"count"`
		}

		// Select the qualified groups
		err := tx.Model((*GroupMemberTracking)(nil)).
			Column("group_id").
			ColumnExpr("array_length(confirmed_users, 1) as count").
			Where("array_length(confirmed_users, 1) >= ?", minUsers).
			For("UPDATE").
			Select(&groups)
		if err != nil {
			r.logger.Error("Failed to get groups with confirmed users",
				zap.Error(err),
				zap.Int("minUsers", minUsers))
			return err
		}

		// Skip if no qualified groups
		if len(groups) == 0 {
			return nil
		}

		// Build the map and list of group IDs
		groupIDs := make([]uint64, len(groups))
		for i, group := range groups {
			groupsMap[group.GroupID] = group.Count
			groupIDs[i] = group.GroupID
		}

		// Delete the selected records
		_, err = tx.Model((*GroupMemberTracking)(nil)).
			Where("group_id IN (?)", pg.In(groupIDs)).
			Delete()
		if err != nil {
			r.logger.Error("Failed to delete qualified group trackings", zap.Error(err))
			return err
		}

		return nil
	})

	return groupsMap, err
}

// GetAndRemoveQualifiedUserTrackings retrieves and removes users that have sufficient confirmed network users.
func (r *TrackingRepository) GetAndRemoveQualifiedUserTrackings(minUsers int) (map[uint64]int, error) {
	usersMap := make(map[uint64]int)

	err := r.db.RunInTransaction(r.db.Context(), func(tx *pg.Tx) error {
		var users []struct {
			UserID uint64 `pg:"user_id"`
			Count  int    `pg:"count"`
		}

		// Select the qualified users
		err := tx.Model((*UserNetworkTracking)(nil)).
			Column("user_id").
			ColumnExpr("array_length(confirmed_users, 1) as count").
			Where("array_length(confirmed_users, 1) >= ?", minUsers).
			For("UPDATE").
			Select(&users)
		if err != nil {
			r.logger.Error("Failed to get users with confirmed network users",
				zap.Error(err),
				zap.Int("minUsers", minUsers))
			return err
		}

		// Skip if no qualified users
		if len(users) == 0 {
			return nil
		}

		// Build the map and list of user IDs
		userIDs := make([]uint64, len(users))
		for i, user := range users {
			usersMap[user.UserID] = user.Count
			userIDs[i] = user.UserID
		}

		// Delete the selected records
		_, err = tx.Model((*UserNetworkTracking)(nil)).
			Where("user_id IN (?)", pg.In(userIDs)).
			Delete()
		if err != nil {
			r.logger.Error("Failed to delete qualified user trackings", zap.Error(err))
			return err
		}

		return nil
	})

	return usersMap, err
}

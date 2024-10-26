package database

import (
	"time"

	"github.com/go-pg/pg/v10"
	"go.uber.org/zap"
)

// TrackingRepository handles operations for group member and user affiliate tracking.
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
		tracking := &GroupMemberTracking{
			GroupID:        groupID,
			ConfirmedUsers: []uint64{userID},
			LastAppended:   time.Now(),
		}

		_, err := tx.Model(tracking).
			OnConflict("(group_id) DO UPDATE").
			Set("confirmed_users = array_append(EXCLUDED.confirmed_users, ?)", userID).
			Set("last_appended = ?", time.Now()).
			Where("NOT ? = ANY(group_member_tracking.confirmed_users)", userID).
			Insert()
		if err != nil {
			r.logger.Error("Failed to add user to group tracking", zap.Error(err), zap.Uint64("groupID", groupID), zap.Uint64("userID", userID))
			return err
		}

		return nil
	})
}

// AddUserToAffiliateTracking adds a confirmed user to the user affiliate tracking.
func (r *TrackingRepository) AddUserToAffiliateTracking(userID, affiliateID uint64) error {
	return r.db.RunInTransaction(r.db.Context(), func(tx *pg.Tx) error {
		tracking := &UserAffiliateTracking{
			UserID:         userID,
			ConfirmedUsers: []uint64{affiliateID},
			LastAppended:   time.Now(),
		}

		_, err := tx.Model(tracking).
			OnConflict("(user_id) DO UPDATE").
			Set("confirmed_users = array_append(EXCLUDED.confirmed_users, ?)", affiliateID).
			Set("last_appended = ?", time.Now()).
			Where("NOT ? = ANY(user_affiliate_tracking.confirmed_users)", affiliateID).
			Insert()
		if err != nil {
			r.logger.Error("Failed to add user to affiliate tracking", zap.Error(err), zap.Uint64("userID", userID), zap.Uint64("affiliateID", affiliateID))
			return err
		}

		return nil
	})
}

// PurgeOldGroupMemberTrackings removes old entries from group_member_trackings.
func (r *TrackingRepository) PurgeOldGroupMemberTrackings(cutoffDate time.Time, batchSize int) (int, error) {
	var result pg.Result
	err := r.db.RunInTransaction(r.db.Context(), func(tx *pg.Tx) error {
		var err error
		result, err = tx.Model((*GroupMemberTracking)(nil)).
			Where("last_appended < ?", cutoffDate).
			Limit(batchSize).
			Delete()
		return err
	})
	if err != nil {
		return 0, err
	}

	return result.RowsAffected(), nil
}

// PurgeOldUserAffiliateTrackings removes old entries from user_affiliate_trackings.
func (r *TrackingRepository) PurgeOldUserAffiliateTrackings(cutoffDate time.Time, batchSize int) (int, error) {
	var result pg.Result
	err := r.db.RunInTransaction(r.db.Context(), func(tx *pg.Tx) error {
		var err error
		result, err = tx.Model((*UserAffiliateTracking)(nil)).
			Where("last_appended < ?", cutoffDate).
			Limit(batchSize).
			Delete()
		return err
	})
	if err != nil {
		return 0, err
	}

	return result.RowsAffected(), nil
}

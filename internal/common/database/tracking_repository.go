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
	_, err := r.db.Exec(`
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
}

// AddUserToAffiliateTracking adds a confirmed user to the user affiliate tracking.
func (r *TrackingRepository) AddUserToAffiliateTracking(userID, affiliateID uint64) error {
	_, err := r.db.Exec(`
		INSERT INTO user_affiliate_trackings (user_id, confirmed_users, last_appended)
		VALUES (?, ARRAY[?], NOW())
		ON CONFLICT (user_id) DO UPDATE
		SET confirmed_users = array_append(user_affiliate_trackings.confirmed_users, EXCLUDED.confirmed_users[1]),
			last_appended = NOW()
		WHERE NOT EXCLUDED.confirmed_users[1] = ANY(user_affiliate_trackings.confirmed_users)
	`, userID, affiliateID)
	if err != nil {
		r.logger.Error("Failed to add user to affiliate tracking", zap.Error(err), zap.Uint64("userID", userID), zap.Uint64("affiliateID", affiliateID))
		return err
	}

	return nil
}

// PurgeOldGroupMemberTrackings removes old entries from group_member_trackings.
func (r *TrackingRepository) PurgeOldGroupMemberTrackings(cutoffDate time.Time, batchSize int) (int, error) {
	result, err := r.db.Exec(`
		DELETE FROM group_member_trackings
		WHERE group_id IN (
			SELECT group_id
			FROM group_member_trackings
			WHERE last_appended < ?
			LIMIT ?
		)
	`, cutoffDate, batchSize)
	if err != nil {
		return 0, err
	}

	return result.RowsAffected(), nil
}

// PurgeOldUserAffiliateTrackings removes old entries from user_affiliate_trackings.
func (r *TrackingRepository) PurgeOldUserAffiliateTrackings(cutoffDate time.Time, batchSize int) (int, error) {
	result, err := r.db.Exec(`
		DELETE FROM user_affiliate_trackings
		WHERE user_id IN (
			SELECT user_id
			FROM user_affiliate_trackings
			WHERE last_appended < ?
			LIMIT ?
		)
	`, cutoffDate, batchSize)
	if err != nil {
		return 0, err
	}

	return result.RowsAffected(), nil
}
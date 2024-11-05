package database

import (
	"context"
	"fmt"
	"time"

	"github.com/go-pg/pg/v10"
	"github.com/rotector/rotector/internal/common/statistics"
	"go.uber.org/zap"
)

const (
	// UserTypeConfirmed indicates a user has been reviewed and confirmed as inappropriate.
	UserTypeConfirmed = "confirmed"
	// UserTypeFlagged indicates a user needs review for potential violations.
	UserTypeFlagged = "flagged"
	// UserTypeCleared indicates a user was reviewed and found to be appropriate.
	UserTypeCleared = "cleared"
)

// UserRepository handles database operations for user records.
type UserRepository struct {
	db       *pg.DB
	stats    *statistics.Client
	tracking *TrackingRepository
	logger   *zap.Logger
}

// NewUserRepository creates a UserRepository with references to the statistics
// and tracking systems for updating related data during user operations.
func NewUserRepository(db *pg.DB, stats *statistics.Client, tracking *TrackingRepository, logger *zap.Logger) *UserRepository {
	return &UserRepository{
		db:       db,
		stats:    stats,
		tracking: tracking,
		logger:   logger,
	}
}

// GetFlaggedUserToReview finds a user to review based on the sort method:
// - random: selects any unviewed user randomly
// - confidence: selects the user with highest confidence score
// - last_updated: selects the oldest updated user.
func (r *UserRepository) GetFlaggedUserToReview(sortBy string) (*FlaggedUser, error) {
	var user FlaggedUser
	err := r.db.RunInTransaction(r.db.Context(), func(tx *pg.Tx) error {
		query := tx.Model(&FlaggedUser{}).
			Where("last_viewed IS NULL OR last_viewed < NOW() - INTERVAL '10 minutes'")

		switch sortBy {
		case SortByConfidence:
			query = query.Order("confidence DESC")
		case SortByLastUpdated:
			query = query.Order("last_updated ASC")
		case SortByRandom:
			query = query.OrderExpr("RANDOM()")
		default:
			return fmt.Errorf("%w: %s", ErrInvalidSortBy, sortBy)
		}

		err := query.Limit(1).
			For("UPDATE SKIP LOCKED").
			Select(&user)
		if err != nil {
			r.logger.Error("Failed to get flagged user to review", zap.Error(err))
			return err
		}

		_, err = tx.Model(&user).
			Set("last_viewed = ?", time.Now()).
			Where("id = ?", user.ID).
			Update()
		if err != nil {
			r.logger.Error("Failed to update last_viewed", zap.Error(err))
			return err
		}
		user.LastViewed = time.Now()

		return nil
	})
	if err != nil {
		return nil, err
	}

	r.logger.Info("Retrieved random flagged user",
		zap.Uint64("userID", user.ID),
		zap.String("sortBy", sortBy),
		zap.Time("lastViewed", user.LastViewed))

	return &user, nil
}

// GetNextConfirmedUser finds the next confirmed user that needs scanning.
func (r *UserRepository) GetNextConfirmedUser() (*ConfirmedUser, error) {
	var user ConfirmedUser
	err := r.db.RunInTransaction(r.db.Context(), func(tx *pg.Tx) error {
		err := tx.Model(&user).
			Where("last_scanned IS NULL OR last_scanned < NOW() - INTERVAL '1 day'").
			Order("last_scanned ASC NULLS FIRST").
			Limit(1).
			For("UPDATE SKIP LOCKED").
			Select()
		if err != nil {
			r.logger.Error("Failed to get next confirmed user", zap.Error(err))
			return err
		}

		_, err = tx.Model(&user).
			Set("last_scanned = ?", time.Now()).
			Where("id = ?", user.ID).
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

	r.logger.Info("Retrieved next confirmed user", zap.Uint64("userID", user.ID))
	return &user, nil
}

// SaveFlaggedUsers adds or updates users in the flagged_users table.
// For each user, it updates all fields if the user already exists,
// or inserts a new record if they don't.
func (r *UserRepository) SaveFlaggedUsers(flaggedUsers []*User) {
	r.logger.Info("Saving flagged users", zap.Int("count", len(flaggedUsers)))

	for _, flaggedUser := range flaggedUsers {
		_, err := r.db.Model(&FlaggedUser{User: *flaggedUser}).
			OnConflict("(id) DO UPDATE").
			Set("name = EXCLUDED.name").
			Set("display_name = EXCLUDED.display_name").
			Set("description = EXCLUDED.description").
			Set("created_at = EXCLUDED.created_at").
			Set("reason = EXCLUDED.reason").
			Set("groups = EXCLUDED.groups").
			Set("outfits = EXCLUDED.outfits").
			Set("friends = EXCLUDED.friends").
			Set("flagged_content = EXCLUDED.flagged_content").
			Set("flagged_groups = EXCLUDED.flagged_groups").
			Set("confidence = EXCLUDED.confidence").
			Set("last_updated = EXCLUDED.last_updated").
			Set("thumbnail_url = EXCLUDED.thumbnail_url").
			Insert()
		if err != nil {
			r.logger.Error("Error saving flagged user",
				zap.Uint64("userID", flaggedUser.ID),
				zap.String("username", flaggedUser.Name),
				zap.String("reason", flaggedUser.Reason),
				zap.Float64("confidence", flaggedUser.Confidence),
				zap.Error(err))
			continue
		}

		r.logger.Info("Saved flagged user",
			zap.Uint64("userID", flaggedUser.ID),
			zap.String("username", flaggedUser.Name),
			zap.String("reason", flaggedUser.Reason),
			zap.Int("groups_count", len(flaggedUser.Groups)),
			zap.Strings("flagged_content", flaggedUser.FlaggedContent),
			zap.Uint64s("flagged_groups", flaggedUser.FlaggedGroups),
			zap.Float64("confidence", flaggedUser.Confidence),
			zap.Time("last_updated", time.Now()),
			zap.String("thumbnail_url", flaggedUser.ThumbnailURL))
	}

	// Update statistics
	if err := r.stats.IncrementDailyStat(context.Background(), statistics.FieldUsersFlagged, len(flaggedUsers)); err != nil {
		r.logger.Error("Failed to increment users_flagged statistic", zap.Error(err))
	}

	if err := r.stats.IncrementHourlyStat(context.Background(), statistics.FieldUsersFlagged, len(flaggedUsers)); err != nil {
		r.logger.Error("Failed to increment hourly flagged stat", zap.Error(err))
	}

	r.logger.Info("Finished saving flagged users")
}

// ConfirmUser moves a user from flagged_users to confirmed_users.
// This happens when a moderator confirms that a user is inappropriate.
// The user's groups and friends are tracked to help identify related users.
func (r *UserRepository) ConfirmUser(user *FlaggedUser) error {
	return r.db.RunInTransaction(r.db.Context(), func(tx *pg.Tx) error {
		confirmedUser := &ConfirmedUser{
			User:       user.User,
			VerifiedAt: time.Now(),
		}

		_, err := tx.Model(confirmedUser).
			OnConflict("(id) DO UPDATE").
			Set("name = EXCLUDED.name").
			Set("display_name = EXCLUDED.display_name").
			Set("description = EXCLUDED.description").
			Set("created_at = EXCLUDED.created_at").
			Set("reason = EXCLUDED.reason").
			Set("groups = EXCLUDED.groups").
			Set("outfits = EXCLUDED.outfits").
			Set("friends = EXCLUDED.friends").
			Set("flagged_content = EXCLUDED.flagged_content").
			Set("flagged_groups = EXCLUDED.flagged_groups").
			Set("confidence = EXCLUDED.confidence").
			Set("last_scanned = EXCLUDED.last_scanned").
			Set("last_updated = EXCLUDED.last_updated").
			Set("last_purge_check = EXCLUDED.last_purge_check").
			Set("thumbnail_url = EXCLUDED.thumbnail_url").
			Set("verified_at = EXCLUDED.verified_at").
			Insert()
		if err != nil {
			r.logger.Error("Failed to insert or update user in confirmed_users", zap.Error(err), zap.Uint64("userID", user.ID))
			return err
		}

		_, err = tx.Model((*FlaggedUser)(nil)).Where("id = ?", user.ID).Delete()
		if err != nil {
			r.logger.Error("Failed to delete user from flagged_users", zap.Error(err), zap.Uint64("userID", user.ID))
			return err
		}

		// Track groups for affiliation analysis
		for _, group := range user.Groups {
			if err = r.tracking.AddUserToGroupTracking(group.Group.ID, user.ID); err != nil {
				r.logger.Error("Failed to add user to group tracking", zap.Error(err), zap.Uint64("groupID", group.Group.ID), zap.Uint64("userID", user.ID))
				return err
			}
		}

		// Update statistics
		if err := r.stats.IncrementDailyStat(tx.Context(), statistics.FieldUsersConfirmed, 1); err != nil {
			r.logger.Error("Failed to increment users_confirmed statistic", zap.Error(err))
			return err
		}

		if err := r.stats.IncrementHourlyStat(tx.Context(), statistics.FieldUsersConfirmed, 1); err != nil {
			r.logger.Error("Failed to increment hourly confirmed stat", zap.Error(err))
		}

		return nil
	})
}

// ClearUser moves a user from flagged_users to cleared_users.
// This happens when a moderator determines that a user was incorrectly flagged.
func (r *UserRepository) ClearUser(user *FlaggedUser) error {
	return r.db.RunInTransaction(r.db.Context(), func(tx *pg.Tx) error {
		clearedUser := &ClearedUser{
			User:      user.User,
			ClearedAt: time.Now(),
		}

		_, err := tx.Model(clearedUser).
			OnConflict("(id) DO UPDATE").
			Set("name = EXCLUDED.name").
			Set("display_name = EXCLUDED.display_name").
			Set("description = EXCLUDED.description").
			Set("created_at = EXCLUDED.created_at").
			Set("reason = EXCLUDED.reason").
			Set("groups = EXCLUDED.groups").
			Set("outfits = EXCLUDED.outfits").
			Set("friends = EXCLUDED.friends").
			Set("flagged_content = EXCLUDED.flagged_content").
			Set("flagged_groups = EXCLUDED.flagged_groups").
			Set("confidence = EXCLUDED.confidence").
			Set("last_scanned = EXCLUDED.last_scanned").
			Set("last_updated = EXCLUDED.last_updated").
			Set("last_purge_check = EXCLUDED.last_purge_check").
			Set("thumbnail_url = EXCLUDED.thumbnail_url").
			Set("cleared_at = EXCLUDED.cleared_at").
			Insert()
		if err != nil {
			r.logger.Error("Failed to insert or update user in cleared_users", zap.Error(err), zap.Uint64("userID", user.ID))
			return err
		}

		_, err = tx.Model((*FlaggedUser)(nil)).Where("id = ?", user.ID).Delete()
		if err != nil {
			r.logger.Error("Failed to delete user from flagged_users", zap.Error(err), zap.Uint64("userID", user.ID))
			return err
		}

		r.logger.Info("User cleared and moved to cleared_users", zap.Uint64("userID", user.ID))

		// Update statistics
		if err := r.stats.IncrementDailyStat(tx.Context(), statistics.FieldUsersCleared, 1); err != nil {
			r.logger.Error("Failed to increment users_cleared statistic", zap.Error(err))
			return err
		}

		if err := r.stats.IncrementHourlyStat(tx.Context(), statistics.FieldUsersCleared, 1); err != nil {
			r.logger.Error("Failed to increment hourly cleared stat", zap.Error(err))
		}

		return nil
	})
}

// GetFlaggedUserByID finds a user in the flagged_users table by their ID.
func (r *UserRepository) GetFlaggedUserByID(id uint64) (*FlaggedUser, error) {
	var user FlaggedUser
	err := r.db.Model(&user).Where("id = ?", id).Select()
	if err != nil {
		r.logger.Error("Failed to get flagged user by ID", zap.Error(err), zap.Uint64("userID", id))
		return nil, err
	}
	r.logger.Info("Retrieved flagged user by ID", zap.Uint64("userID", id))
	return &user, nil
}

// GetClearedUserByID finds a user in the cleared_users table by their ID.
func (r *UserRepository) GetClearedUserByID(id uint64) (*ClearedUser, error) {
	var user ClearedUser
	err := r.db.Model(&user).Where("id = ?", id).Select()
	if err != nil {
		r.logger.Error("Failed to get cleared user by ID", zap.Error(err), zap.Uint64("userID", id))
		return nil, err
	}
	r.logger.Info("Retrieved cleared user by ID", zap.Uint64("userID", id))
	return &user, nil
}

// GetConfirmedUsersCount returns the total number of users in confirmed_users.
func (r *UserRepository) GetConfirmedUsersCount() (int, error) {
	var count int
	_, err := r.db.QueryOne(pg.Scan(&count), "SELECT COUNT(*) FROM confirmed_users")
	if err != nil {
		r.logger.Error("Failed to get confirmed users count", zap.Error(err))
		return 0, err
	}
	return count, nil
}

// GetFlaggedUsersCount returns the total number of users in flagged_users.
func (r *UserRepository) GetFlaggedUsersCount() (int, error) {
	var count int
	_, err := r.db.QueryOne(pg.Scan(&count), "SELECT COUNT(*) FROM flagged_users")
	if err != nil {
		r.logger.Error("Failed to get flagged users count", zap.Error(err))
		return 0, err
	}
	return count, nil
}

// GetClearedUsersCount returns the total number of users in cleared_users.
func (r *UserRepository) GetClearedUsersCount() (int, error) {
	var count int
	_, err := r.db.QueryOne(pg.Scan(&count), "SELECT COUNT(*) FROM cleared_users")
	if err != nil {
		r.logger.Error("Failed to get cleared users count", zap.Error(err))
		return 0, err
	}
	return count, nil
}

// CheckExistingUsers finds which users from a list of IDs exist in any user table.
// Returns a map of user IDs to their status (confirmed, flagged, cleared).
func (r *UserRepository) CheckExistingUsers(userIDs []uint64) (map[uint64]string, error) {
	var users []struct {
		ID     uint64
		Status string
	}

	err := r.db.RunInTransaction(r.db.Context(), func(tx *pg.Tx) error {
		return tx.Model((*ConfirmedUser)(nil)).
			Column("id").
			ColumnExpr("? AS status", UserTypeConfirmed).
			Where("id IN (?)", pg.In(userIDs)).
			Union(
				tx.Model((*FlaggedUser)(nil)).
					Column("id").
					ColumnExpr("? AS status", UserTypeFlagged).
					Where("id IN (?)", pg.In(userIDs)),
			).
			Union(
				tx.Model((*ClearedUser)(nil)).
					Column("id").
					ColumnExpr("? AS status", UserTypeCleared).
					Where("id IN (?)", pg.In(userIDs)),
			).
			Select(&users)
	})
	if err != nil {
		r.logger.Error("Failed to check existing users", zap.Error(err))
		return nil, err
	}

	result := make(map[uint64]string, len(users))
	for _, user := range users {
		result[user.ID] = user.Status
	}

	r.logger.Debug("Checked existing users",
		zap.Int("total", len(userIDs)),
		zap.Int("existing", len(result)))

	return result, nil
}

// GetUsersToCheck finds users that haven't been checked for banned status recently.
// Returns a batch of user IDs and updates their last_purge_check timestamp.
func (r *UserRepository) GetUsersToCheck(limit int) ([]uint64, error) {
	var userIDs []uint64

	err := r.db.RunInTransaction(r.db.Context(), func(tx *pg.Tx) error {
		// Use CTE to select users
		_, err := tx.Query(&userIDs, `
			WITH selected_users AS (
				(
					SELECT id, 'confirmed' as type
					FROM confirmed_users
					WHERE last_purge_check IS NULL 
						OR last_purge_check < NOW() - INTERVAL '8 hours'
					ORDER BY RANDOM()
					LIMIT ?
				)
				UNION ALL
				(
					SELECT id, 'flagged' as type
					FROM flagged_users
					WHERE last_purge_check IS NULL 
						OR last_purge_check < NOW() - INTERVAL '8 hours'
					ORDER BY RANDOM()
					LIMIT ?
				)
			)
			SELECT id FROM selected_users
		`, limit/2, limit/2)
		if err != nil {
			r.logger.Error("Failed to get users to check", zap.Error(err))
			return err
		}

		// Update last_purge_check for selected users
		if len(userIDs) > 0 {
			_, err = tx.Model((*ConfirmedUser)(nil)).
				Set("last_purge_check = ?", time.Now()).
				Where("id IN (?)", pg.In(userIDs)).
				Update()
			if err != nil {
				r.logger.Error("Failed to update last_purge_check for confirmed users", zap.Error(err))
				return err
			}

			_, err = tx.Model((*FlaggedUser)(nil)).
				Set("last_purge_check = ?", time.Now()).
				Where("id IN (?)", pg.In(userIDs)).
				Update()
			if err != nil {
				r.logger.Error("Failed to update last_purge_check for flagged users", zap.Error(err))
				return err
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	r.logger.Info("Retrieved and updated users to check", zap.Int("count", len(userIDs)))
	return userIDs, nil
}

// RemoveBannedUsers moves users from confirmed_users and flagged_users to banned_users.
// This happens when users are found to be banned by Roblox.
func (r *UserRepository) RemoveBannedUsers(userIDs []uint64) error {
	return r.db.RunInTransaction(r.db.Context(), func(tx *pg.Tx) error {
		// Move confirmed users to banned_users
		var confirmedUsers []ConfirmedUser
		err := tx.Model(&confirmedUsers).
			Where("id IN (?)", pg.In(userIDs)).
			Select()
		if err != nil {
			r.logger.Error("Failed to select confirmed users for banning", zap.Error(err))
			return err
		}

		for _, user := range confirmedUsers {
			bannedUser := &BannedUser{
				User:     user.User,
				PurgedAt: time.Now(),
			}
			_, err = tx.Model(bannedUser).
				OnConflict("(id) DO UPDATE").
				Insert()
			if err != nil {
				r.logger.Error("Failed to insert banned user from confirmed_users", zap.Error(err), zap.Uint64("userID", user.ID))
				return err
			}
		}

		// Move flagged users to banned_users
		var flaggedUsers []FlaggedUser
		err = tx.Model(&flaggedUsers).
			Where("id IN (?)", pg.In(userIDs)).
			Select()
		if err != nil {
			r.logger.Error("Failed to select flagged users for banning", zap.Error(err))
			return err
		}

		for _, user := range flaggedUsers {
			bannedUser := &BannedUser{
				User:     user.User,
				PurgedAt: time.Now(),
			}
			_, err = tx.Model(bannedUser).
				OnConflict("(id) DO UPDATE").
				Insert()
			if err != nil {
				r.logger.Error("Failed to insert banned user from flagged_users", zap.Error(err), zap.Uint64("userID", user.ID))
				return err
			}
		}

		// Remove users from confirmed_users
		_, err = tx.Model((*ConfirmedUser)(nil)).
			Where("id IN (?)", pg.In(userIDs)).
			Delete()
		if err != nil {
			r.logger.Error("Failed to remove banned users from confirmed_users", zap.Error(err))
			return err
		}

		// Remove users from flagged_users
		_, err = tx.Model((*FlaggedUser)(nil)).
			Where("id IN (?)", pg.In(userIDs)).
			Delete()
		if err != nil {
			r.logger.Error("Failed to remove banned users from flagged_users", zap.Error(err))
			return err
		}

		// Update statistics
		if err := r.stats.IncrementDailyStat(tx.Context(), statistics.FieldBannedUsersPurged, len(userIDs)); err != nil {
			r.logger.Error("Failed to increment banned_users_purged statistic", zap.Error(err))
			return err
		}

		r.logger.Info("Moved banned users to banned_users", zap.Int("count", len(userIDs)))
		return nil
	})
}

// PurgeOldClearedUsers removes cleared users older than the cutoff date.
// This helps maintain database size by removing users that were cleared long ago.
func (r *UserRepository) PurgeOldClearedUsers(cutoffDate time.Time, limit int) (int, error) {
	var affected int

	err := r.db.RunInTransaction(r.db.Context(), func(tx *pg.Tx) error {
		// Select users to purge
		var userIDs []uint64
		err := tx.Model((*ClearedUser)(nil)).
			Column("id").
			Where("cleared_at < ?", cutoffDate).
			Order("cleared_at ASC").
			Limit(limit).
			For("UPDATE SKIP LOCKED").
			Select(&userIDs)
		if err != nil {
			r.logger.Error("Failed to get cleared users to purge",
				zap.Error(err),
				zap.Time("cutoffDate", cutoffDate))
			return err
		}

		// Skip if no users to purge
		if len(userIDs) == 0 {
			return nil
		}

		// Delete selected users
		result, err := tx.Model((*ClearedUser)(nil)).
			Where("id IN (?)", pg.In(userIDs)).
			Delete()
		if err != nil {
			r.logger.Error("Failed to delete users from cleared_users",
				zap.Error(err),
				zap.Uint64s("userIDs", userIDs))
			return err
		}

		affected = result.RowsAffected()

		r.logger.Info("Purged cleared users from database",
			zap.Int("count", affected),
			zap.Uint64s("userIDs", userIDs))

		// Update statistics
		if err := r.stats.IncrementDailyStat(tx.Context(), statistics.FieldClearedUsersPurged, affected); err != nil {
			r.logger.Error("Failed to increment cleared_users_purged statistic", zap.Error(err))
			return err
		}

		return nil
	})

	return affected, err
}

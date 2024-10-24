package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/bytedance/sonic"
	"github.com/go-pg/pg/v10"
	"github.com/rotector/rotector/internal/common/statistics"
	"go.uber.org/zap"
)

// UserRepository handles user-related database operations.
type UserRepository struct {
	db     *pg.DB
	stats  *statistics.Statistics
	logger *zap.Logger
}

// NewUserRepository creates a new UserRepository instance.
func NewUserRepository(db *pg.DB, stats *statistics.Statistics, logger *zap.Logger) *UserRepository {
	return &UserRepository{
		db:     db,
		stats:  stats,
		logger: logger,
	}
}

// GetRandomFlaggedUser retrieves a random flagged user based on the specified sorting criteria.
func (r *UserRepository) GetRandomFlaggedUser(sortBy string) (*FlaggedUser, error) {
	var user struct {
		FlaggedUser
		OldLastReviewed time.Time `pg:"old_last_reviewed"`
	}

	query := `
        WITH sampled_users AS (
            SELECT id, last_reviewed
            FROM flagged_users
            WHERE last_reviewed IS NULL OR last_reviewed < NOW() - INTERVAL '5 minutes'
        ),
        selected_user AS (
            SELECT id, last_reviewed
            FROM sampled_users
    `
	switch sortBy {
	case SortByConfidence:
		query += "ORDER BY (SELECT confidence FROM flagged_users WHERE id = sampled_users.id) DESC"
	case SortByLastUpdated:
		query += "ORDER BY (SELECT last_updated FROM flagged_users WHERE id = sampled_users.id) DESC"
	case SortByRandom:
		query += "ORDER BY RANDOM()"
	default:
		return nil, fmt.Errorf("%w: %s", ErrInvalidSortBy, sortBy)
	}
	query += `
            LIMIT 1
        )
        UPDATE flagged_users
        SET last_reviewed = NOW()
        WHERE id = (SELECT id FROM selected_user)
        RETURNING *, (SELECT last_reviewed FROM selected_user) AS old_last_reviewed
    `

	if _, err := r.db.QueryOne(&user, query); err != nil {
		if errors.Is(err, pg.ErrNoRows) {
			r.logger.Info("No flagged users available for review")
			return nil, err
		}
		r.logger.Error("Failed to get random flagged user", zap.Error(err))
		return nil, err
	}

	r.logger.Info("Retrieved random flagged user",
		zap.Uint64("userID", user.ID),
		zap.String("sortBy", sortBy),
		zap.Time("oldLastReviewed", user.OldLastReviewed))

	// Replace the LastReviewed value with the old value
	user.FlaggedUser.LastReviewed = user.OldLastReviewed

	return &user.FlaggedUser, nil
}

// GetNextConfirmedUser retrieves the next confirmed user to be processed.
func (r *UserRepository) GetNextConfirmedUser() (*ConfirmedUser, error) {
	var user ConfirmedUser
	_, err := r.db.QueryOne(&user, `
		UPDATE confirmed_users
		SET last_scanned = NOW()
		WHERE id = (
			SELECT id
			FROM confirmed_users
			WHERE last_scanned IS NULL OR last_scanned < NOW() - INTERVAL '1 day'
			ORDER BY last_scanned ASC NULLS FIRST
			LIMIT 1
		)
		RETURNING *
	`)
	if err != nil {
		r.logger.Error("Failed to get next confirmed user", zap.Error(err))
		return nil, err
	}
	r.logger.Info("Retrieved next confirmed user", zap.Uint64("userID", user.ID))
	return &user, nil
}

// SaveFlaggedUsers saves or updates the provided flagged users in the database.
func (r *UserRepository) SaveFlaggedUsers(flaggedUsers []*User) {
	r.logger.Info("Saving flagged users", zap.Int("count", len(flaggedUsers)))

	for _, flaggedUser := range flaggedUsers {
		flaggedContentJSON, err := sonic.Marshal(flaggedUser.FlaggedContent)
		if err != nil {
			r.logger.Error("Error marshaling flagged content",
				zap.Uint64("userID", flaggedUser.ID),
				zap.String("username", flaggedUser.Name),
				zap.Error(err))
			continue
		}

		flaggedGroupJSON, err := sonic.Marshal(flaggedUser.FlaggedGroups)
		if err != nil {
			r.logger.Error("Error marshaling flagged groups",
				zap.Uint64("userID", flaggedUser.ID),
				zap.String("username", flaggedUser.Name),
				zap.Error(err))
			continue
		}

		groupsJSON, err := sonic.Marshal(flaggedUser.Groups)
		if err != nil {
			r.logger.Error("Error marshaling user groups",
				zap.Uint64("userID", flaggedUser.ID),
				zap.String("username", flaggedUser.Name),
				zap.Error(err))
			continue
		}

		outfitsJSON, err := sonic.Marshal(flaggedUser.Outfits)
		if err != nil {
			r.logger.Error("Error marshaling user outfits",
				zap.Uint64("userID", flaggedUser.ID),
				zap.String("username", flaggedUser.Name),
				zap.Error(err))
			continue
		}

		friendsJSON, err := sonic.Marshal(flaggedUser.Friends)
		if err != nil {
			r.logger.Error("Error marshaling user friends",
				zap.Uint64("userID", flaggedUser.ID),
				zap.String("username", flaggedUser.Name),
				zap.Error(err))
			continue
		}

		_, err = r.db.Exec(`
			WITH user_check AS (
				SELECT id FROM confirmed_users WHERE id = ?
			)
			INSERT INTO flagged_users (
				id, name, display_name, description, created_at, reason,
				groups, outfits, friends, flagged_content, confirmed_groups,
				confidence, last_updated, thumbnail_url
			)
			SELECT ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NOW(), ?
			WHERE NOT EXISTS (SELECT 1 FROM user_check)
			ON CONFLICT (id) DO UPDATE 
			SET name = EXCLUDED.name,
				display_name = EXCLUDED.display_name,
				description = EXCLUDED.description,
				created_at = EXCLUDED.created_at,
				reason = EXCLUDED.reason,
				groups = EXCLUDED.groups,
				outfits = EXCLUDED.outfits,
				friends = EXCLUDED.friends,
				flagged_content = EXCLUDED.flagged_content,
				confirmed_groups = EXCLUDED.confirmed_groups,
				confidence = EXCLUDED.confidence,
				last_updated = NOW(),
				thumbnail_url = EXCLUDED.thumbnail_url
		`,
			flaggedUser.ID, flaggedUser.ID, flaggedUser.Name, flaggedUser.DisplayName,
			flaggedUser.Description, flaggedUser.CreatedAt, flaggedUser.Reason,
			string(groupsJSON), string(outfitsJSON), string(friendsJSON),
			string(flaggedContentJSON), string(flaggedGroupJSON),
			flaggedUser.Confidence, flaggedUser.ThumbnailURL)
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
			zap.Uint64s("confirmed_groups", flaggedUser.FlaggedGroups),
			zap.Float64("confidence", flaggedUser.Confidence),
			zap.Time("last_updated", time.Now()),
			zap.String("thumbnail_url", flaggedUser.ThumbnailURL))
	}

	// Increment the users_flagged statistic
	if err := r.stats.IncrementUsersFlagged(context.Background(), len(flaggedUsers)); err != nil {
		r.logger.Error("Failed to increment users_flagged statistic", zap.Error(err))
	}

	r.logger.Info("Finished saving flagged users")
}

// BanUser moves a user from flagged to confirmedstatus.
func (r *UserRepository) BanUser(user *FlaggedUser) error {
	_, err := r.db.Exec(`
		INSERT INTO confirmed_users (
			id, name, display_name, description, created_at, reason,
			groups, outfits, friends, flagged_content, flagged_groups,
			confidence, last_scanned, last_updated, last_reviewed, thumbnail_url,
			verified_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NOW(), ?, NOW())
	`, user.ID, user.Name, user.DisplayName, user.Description, user.CreatedAt,
		user.Reason, user.Groups, user.Outfits, user.Friends, user.FlaggedContent,
		user.FlaggedGroups, user.Confidence, user.LastScanned, user.LastUpdated,
		user.ThumbnailURL)
	if err != nil {
		r.logger.Error("Failed to insert user into confirmed_users", zap.Error(err), zap.Uint64("userID", user.ID))
		return err
	}

	_, err = r.db.Exec("DELETE FROM flagged_users WHERE id = ?", user.ID)
	if err != nil {
		r.logger.Error("Failed to delete user from flagged_users", zap.Error(err), zap.Uint64("userID", user.ID))
		return err
	}

	r.logger.Info("User accepted and moved to confirmed_users", zap.Uint64("userID", user.ID))

	// Increment the users_banned statistic
	if err := r.stats.IncrementUsersBanned(context.Background(), 1); err != nil {
		r.logger.Error("Failed to increment users_banned statistic", zap.Error(err))
	}

	return nil
}

// ClearUser removes a user from the flagged users list.
func (r *UserRepository) ClearUser(user *FlaggedUser) error {
	_, err := r.db.Exec("DELETE FROM flagged_users WHERE id = ?", user.ID)
	if err != nil {
		r.logger.Error("Failed to delete user from flagged_users", zap.Error(err), zap.Uint64("userID", user.ID))
		return err
	}
	r.logger.Info("User rejected and removed from flagged_users", zap.Uint64("userID", user.ID))

	// Increment the users_cleared statistic
	if err := r.stats.IncrementUsersCleared(context.Background(), 1); err != nil {
		r.logger.Error("Failed to increment users_cleared statistic", zap.Error(err))
	}

	return nil
}

// GetFlaggedUserByID retrieves a flagged user by their ID.
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

// GetConfirmedUsersCount returns the number of users in the confirmed_users table.
func (r *UserRepository) GetConfirmedUsersCount() (int, error) {
	var count int
	_, err := r.db.QueryOne(pg.Scan(&count), "SELECT COUNT(*) FROM confirmed_users")
	if err != nil {
		r.logger.Error("Failed to get confirmed users count", zap.Error(err))
		return 0, err
	}
	return count, nil
}

// GetFlaggedUsersCount returns the number of users in the flagged_users table.
func (r *UserRepository) GetFlaggedUsersCount() (int, error) {
	var count int
	_, err := r.db.QueryOne(pg.Scan(&count), "SELECT COUNT(*) FROM flagged_users")
	if err != nil {
		r.logger.Error("Failed to get flagged users count", zap.Error(err))
		return 0, err
	}
	return count, nil
}

// CheckExistingUsers checks which of the provided user IDs exist in the database.
func (r *UserRepository) CheckExistingUsers(userIDs []uint64) (map[uint64]string, error) {
	result := make(map[uint64]string)

	var users []struct {
		ID     uint64
		Status string
	}

	_, err := r.db.Query(&users, `
		SELECT id, 'confirmed' AS status FROM confirmed_users WHERE id = ANY(?)
		UNION ALL
		SELECT id, 'flagged' AS status FROM flagged_users WHERE id = ANY(?)
	`, pg.Array(userIDs), pg.Array(userIDs))

	if err != nil && !errors.Is(err, pg.ErrNoRows) {
		r.logger.Error("Failed to check existing users", zap.Error(err))
		return nil, err
	}

	for _, user := range users {
		result[user.ID] = user.Status
	}

	r.logger.Info("Checked existing users",
		zap.Int("total", len(userIDs)),
		zap.Int("existing", len(result)))

	return result, nil
}

// GetUsersToCheck retrieves a batch of users to check for banned status and updates their last_purge_check.
func (r *UserRepository) GetUsersToCheck(limit int) ([]uint64, error) {
	var userIDs []uint64

	// Select the users to check
	_, err := r.db.Query(&userIDs, `
		SELECT id FROM (
			SELECT id FROM confirmed_users
			WHERE last_purge_check IS NULL OR last_purge_check < NOW() - INTERVAL '8 hours'
			UNION ALL
			SELECT id FROM flagged_users
			WHERE last_purge_check IS NULL OR last_purge_check < NOW() - INTERVAL '8 hours'
		) AS users
		ORDER BY RANDOM()
		LIMIT ?
	`, limit)
	if err != nil {
		r.logger.Error("Failed to get users to check", zap.Error(err))
		return nil, err
	}

	// If we have users to check, update their last_purge_check
	if len(userIDs) > 0 {
		_, err = r.db.Exec(`
			UPDATE confirmed_users
			SET last_purge_check = NOW()
			WHERE id = ANY(?);

			UPDATE flagged_users
			SET last_purge_check = NOW()
			WHERE id = ANY(?);
		`, pg.Array(userIDs), pg.Array(userIDs))
		if err != nil {
			r.logger.Error("Failed to update last_purge_check", zap.Error(err))
			return nil, err
		}
	}

	r.logger.Info("Retrieved and updated users to check", zap.Int("count", len(userIDs)))
	return userIDs, nil
}

// RemoveBannedUsers removes the specified banned users from confirmed and flagged tables.
func (r *UserRepository) RemoveBannedUsers(userIDs []uint64) error {
	_, err := r.db.Exec(`
		DELETE FROM confirmed_users WHERE id = ANY(?);
		DELETE FROM flagged_users WHERE id = ANY(?);
	`, pg.Array(userIDs), pg.Array(userIDs))
	if err != nil {
		r.logger.Error("Failed to remove banned users", zap.Error(err))
		return err
	}

	// Increment the users_purged statistic
	if err := r.stats.IncrementUsersPurged(context.Background(), len(userIDs)); err != nil {
		r.logger.Error("Failed to increment users_purged statistic", zap.Error(err))
	}

	r.logger.Info("Removed banned users", zap.Int("count", len(userIDs)))

	return nil
}

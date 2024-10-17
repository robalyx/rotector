package database

import (
	"errors"
	"fmt"
	"time"

	"github.com/bytedance/sonic"
	"github.com/go-pg/pg/v10"
	"go.uber.org/zap"
)

// UserRepository handles user-related database operations.
type UserRepository struct {
	db     *pg.DB
	logger *zap.Logger
}

// NewUserRepository creates a new UserRepository instance.
func NewUserRepository(db *pg.DB, logger *zap.Logger) *UserRepository {
	return &UserRepository{
		db:     db,
		logger: logger,
	}
}

// GetRandomPendingUser retrieves a random pending user based on the specified sorting criteria.
func (r *UserRepository) GetRandomPendingUser(sortBy string) (*PendingUser, error) {
	var user struct {
		PendingUser
		OldLastReviewed time.Time `pg:"old_last_reviewed"`
	}

	query := `
        WITH sampled_users AS (
            SELECT id, last_reviewed
            FROM pending_users
            WHERE last_reviewed IS NULL OR last_reviewed < NOW() - INTERVAL '5 minutes'
        ),
        selected_user AS (
            SELECT id, last_reviewed
            FROM sampled_users
    `
	switch sortBy {
	case SortByConfidence:
		query += "ORDER BY (SELECT confidence FROM pending_users WHERE id = sampled_users.id) DESC"
	case SortByLastUpdated:
		query += "ORDER BY (SELECT last_updated FROM pending_users WHERE id = sampled_users.id) DESC"
	case SortByRandom:
		query += "ORDER BY RANDOM()"
	default:
		return nil, fmt.Errorf("%w: %s", ErrInvalidSortBy, sortBy)
	}
	query += `
            LIMIT 1
        )
        UPDATE pending_users
        SET last_reviewed = NOW()
        WHERE id = (SELECT id FROM selected_user)
        RETURNING *, (SELECT last_reviewed FROM selected_user) AS old_last_reviewed
    `

	if _, err := r.db.QueryOne(&user, query); err != nil {
		if errors.Is(err, pg.ErrNoRows) {
			r.logger.Info("No pending users available for review")
			return nil, err
		}
		r.logger.Error("Failed to get random pending user", zap.Error(err))
		return nil, err
	}

	r.logger.Info("Retrieved random pending user",
		zap.Uint64("userID", user.ID),
		zap.String("sortBy", sortBy),
		zap.Time("oldLastReviewed", user.OldLastReviewed))

	// Replace the LastReviewed value with the old value
	user.PendingUser.LastReviewed = user.OldLastReviewed

	return &user.PendingUser, nil
}

// GetNextFlaggedUser retrieves the next flagged user to be processed.
func (r *UserRepository) GetNextFlaggedUser() (*FlaggedUser, error) {
	var user FlaggedUser
	_, err := r.db.QueryOne(&user, `
		UPDATE flagged_users
		SET last_scanned = NOW()
		WHERE id = (
			SELECT id
			FROM flagged_users
			WHERE last_scanned IS NULL OR last_scanned < NOW() - INTERVAL '1 day'
			ORDER BY last_scanned ASC NULLS FIRST
			LIMIT 1
		)
		RETURNING *
	`)
	if err != nil {
		r.logger.Error("Failed to get next flagged user", zap.Error(err))
		return nil, err
	}
	r.logger.Info("Retrieved next flagged user", zap.Uint64("userID", user.ID))
	return &user, nil
}

// SavePendingUsers saves or updates the provided flagged users in the database.
func (r *UserRepository) SavePendingUsers(flaggedUsers []*User) {
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
				SELECT id FROM flagged_users WHERE id = ?
			)
			INSERT INTO pending_users (
				id, name, display_name, description, created_at, reason,
				groups, outfits, friends, flagged_content, flagged_groups,
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
				flagged_groups = EXCLUDED.flagged_groups,
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
			zap.Uint64s("flagged_groups", flaggedUser.FlaggedGroups),
			zap.Float64("confidence", flaggedUser.Confidence),
			zap.Time("last_updated", time.Now()),
			zap.String("thumbnail_url", flaggedUser.ThumbnailURL))
	}

	r.logger.Info("Finished saving flagged users")
}

// AcceptUser moves a user from pending to flagged status.
func (r *UserRepository) AcceptUser(user *PendingUser) error {
	_, err := r.db.Exec(`
		INSERT INTO flagged_users (id, name, description, reason, flagged_content, confidence, last_scanned, last_updated, thumbnail_url, verified_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, NOW())
	`, user.ID, user.Name, user.Description, user.Reason, user.FlaggedContent, user.Confidence, user.LastScanned, user.LastUpdated, user.ThumbnailURL)
	if err != nil {
		r.logger.Error("Failed to insert user into flagged_users", zap.Error(err), zap.Uint64("userID", user.ID))
		return err
	}

	_, err = r.db.Exec("DELETE FROM pending_users WHERE id = ?", user.ID)
	if err != nil {
		r.logger.Error("Failed to delete user from pending_users", zap.Error(err), zap.Uint64("userID", user.ID))
		return err
	}

	r.logger.Info("User accepted and moved to flagged_users", zap.Uint64("userID", user.ID))
	return nil
}

// RejectUser removes a user from the pending users list.
func (r *UserRepository) RejectUser(user *PendingUser) error {
	_, err := r.db.Exec("DELETE FROM pending_users WHERE id = ?", user.ID)
	if err != nil {
		r.logger.Error("Failed to delete user from pending_users", zap.Error(err), zap.Uint64("userID", user.ID))
		return err
	}
	r.logger.Info("User rejected and removed from pending_users", zap.Uint64("userID", user.ID))
	return nil
}

// GetPendingUserByID retrieves a pending user by their ID.
func (r *UserRepository) GetPendingUserByID(id uint64) (*PendingUser, error) {
	var user PendingUser
	err := r.db.Model(&user).Where("id = ?", id).Select()
	if err != nil {
		r.logger.Error("Failed to get pending user by ID", zap.Error(err), zap.Uint64("userID", id))
		return nil, err
	}
	r.logger.Info("Retrieved pending user by ID", zap.Uint64("userID", id))
	return &user, nil
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

// GetPendingUsersCount returns the number of users in the pending_users table.
func (r *UserRepository) GetPendingUsersCount() (int, error) {
	var count int
	_, err := r.db.QueryOne(pg.Scan(&count), "SELECT COUNT(*) FROM pending_users")
	if err != nil {
		r.logger.Error("Failed to get pending users count", zap.Error(err))
		return 0, err
	}
	return count, nil
}

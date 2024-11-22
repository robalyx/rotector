package models

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/uptrace/bun"
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

// ErrInvalidSortBy indicates that the provided sort method is not supported.
var ErrInvalidSortBy = errors.New("invalid sortBy value")

// ExtendedFriend contains additional user information beyond the basic Friend type.
type ExtendedFriend struct {
	types.Friend            // Embed the base Friend type
	Name             string // Username of the friend
	DisplayName      string // Display name of the friend
	HasVerifiedBadge bool   // Whether the friend has a verified badge
}

// User combines all the information needed to review a user.
// This base structure is embedded in other user types (Flagged, Confirmed).
type User struct {
	ID             uint64                  `bun:",pk"        json:"id"`
	Name           string                  `bun:",notnull"   json:"name"`
	DisplayName    string                  `bun:",notnull"   json:"displayName"`
	Description    string                  `bun:",notnull"   json:"description"`
	CreatedAt      time.Time               `bun:",notnull"   json:"createdAt"`
	Reason         string                  `bun:",notnull"   json:"reason"`
	Groups         []*types.UserGroupRoles `bun:"type:jsonb" json:"groups"`
	Outfits        []types.Outfit          `bun:"type:jsonb" json:"outfits"`
	Friends        []ExtendedFriend        `bun:"type:jsonb" json:"friends"`
	Games          []*types.Game           `bun:"type:jsonb" json:"games"`
	FlaggedContent []string                `bun:"type:jsonb" json:"flaggedContent"`
	FlaggedGroups  []uint64                `bun:"type:jsonb" json:"flaggedGroups"`
	Confidence     float64                 `bun:",notnull"   json:"confidence"`
	LastScanned    time.Time               `bun:",notnull"   json:"lastScanned"`
	LastUpdated    time.Time               `bun:",notnull"   json:"lastUpdated"`
	LastViewed     time.Time               `bun:",notnull"   json:"lastViewed"`
	LastPurgeCheck time.Time               `bun:",notnull"   json:"lastPurgeCheck"`
	ThumbnailURL   string                  `bun:",notnull"   json:"thumbnailUrl"`
	Upvotes        int                     `bun:",notnull"   json:"upvotes"`
	Downvotes      int                     `bun:",notnull"   json:"downvotes"`
	Reputation     int                     `bun:",notnull"   json:"reputation"`
}

// FlaggedUser extends User to track users that need review.
// The base User structure contains all the fields needed for review.
type FlaggedUser struct {
	User
}

// ConfirmedUser extends User to track users that have been reviewed and confirmed.
// The VerifiedAt field shows when the user was confirmed by a moderator.
type ConfirmedUser struct {
	User
	VerifiedAt time.Time `bun:",notnull" json:"verifiedAt"`
}

// ClearedUser extends User to track users that were cleared during review.
// The ClearedAt field shows when the user was cleared by a moderator.
type ClearedUser struct {
	User
	ClearedAt time.Time `bun:",notnull" json:"clearedAt"`
}

// BannedUser extends User to track users that were banned and removed.
// The PurgedAt field shows when the user was removed from the system.
type BannedUser struct {
	User
	PurgedAt time.Time `bun:",notnull" json:"purgedAt"`
}

// UserModel handles database operations for user records.
type UserModel struct {
	db       *bun.DB
	tracking *TrackingModel
	logger   *zap.Logger
}

// NewUser creates a UserModel with references to the tracking system.
func NewUser(db *bun.DB, tracking *TrackingModel, logger *zap.Logger) *UserModel {
	return &UserModel{
		db:       db,
		tracking: tracking,
		logger:   logger,
	}
}

// GetFlaggedUserToReview finds a user to review based on the sort method:
// - random: selects any unviewed user randomly
// - confidence: selects the user with highest confidence score
// - last_updated: selects the oldest updated user
// - reputation: selects the user with highest upvotes - downvotes.
func (r *UserModel) GetFlaggedUserToReview(ctx context.Context, sortBy string) (*FlaggedUser, error) {
	var user FlaggedUser
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		query := tx.NewSelect().Model(&user).
			Where("last_viewed IS NULL OR last_viewed < NOW() - INTERVAL '10 minutes'")

		switch sortBy {
		case SortByConfidence:
			query.Order("confidence DESC")
		case SortByLastUpdated:
			query.Order("last_updated ASC")
		case SortByReputation:
			query.Order("reputation DESC")
		case SortByRandom:
			query.OrderExpr("RANDOM()")
		default:
			return fmt.Errorf("%w: %s", ErrInvalidSortBy, sortBy)
		}

		err := query.Limit(1).
			For("UPDATE SKIP LOCKED").
			Scan(ctx)
		if err != nil {
			r.logger.Error("Failed to get flagged user to review", zap.Error(err))
			return err
		}

		// Update last_viewed
		now := time.Now()
		_, err = tx.NewUpdate().Model(&user).
			Set("last_viewed = ?", now).
			Where("id = ?", user.ID).
			Exec(ctx)
		if err != nil {
			r.logger.Error("Failed to update last_viewed", zap.Error(err))
			return err
		}
		user.LastViewed = now

		return nil
	})
	if err != nil {
		return nil, err
	}

	r.logger.Debug("Retrieved flagged user to review",
		zap.Uint64("userID", user.ID),
		zap.String("sortBy", sortBy),
		zap.Time("lastViewed", user.LastViewed))

	return &user, nil
}

// GetNextConfirmedUser finds the next confirmed user that needs scanning.
func (r *UserModel) GetNextConfirmedUser(ctx context.Context) (*ConfirmedUser, error) {
	var user ConfirmedUser
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		err := tx.NewSelect().Model(&user).
			Where("last_scanned IS NULL OR last_scanned < NOW() - INTERVAL '1 day'").
			Order("last_scanned ASC NULLS FIRST").
			Limit(1).
			For("UPDATE SKIP LOCKED").
			Scan(ctx)
		if err != nil {
			r.logger.Error("Failed to get next confirmed user", zap.Error(err))
			return err
		}

		_, err = tx.NewUpdate().Model(&user).
			Set("last_scanned = ?", time.Now()).
			Where("id = ?", user.ID).
			Exec(ctx)
		if err != nil {
			r.logger.Error("Failed to update last_scanned", zap.Error(err))
			return err
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	r.logger.Debug("Retrieved next confirmed user", zap.Uint64("userID", user.ID))
	return &user, nil
}

// SaveFlaggedUsers adds or updates users in the flagged_users table.
// For each user, it updates all fields if the user already exists,
// or inserts a new record if they don't.
func (r *UserModel) SaveFlaggedUsers(ctx context.Context, flaggedUsers []*User) {
	r.logger.Debug("Saving flagged users", zap.Int("count", len(flaggedUsers)))

	for _, flaggedUser := range flaggedUsers {
		_, err := r.db.NewInsert().Model(&FlaggedUser{User: *flaggedUser}).
			On("CONFLICT (id) DO UPDATE").
			Set("name = EXCLUDED.name").
			Set("display_name = EXCLUDED.display_name").
			Set("description = EXCLUDED.description").
			Set("created_at = EXCLUDED.created_at").
			Set("reason = EXCLUDED.reason").
			Set("groups = EXCLUDED.groups").
			Set("outfits = EXCLUDED.outfits").
			Set("friends = EXCLUDED.friends").
			Set("games = EXCLUDED.games").
			Set("flagged_content = EXCLUDED.flagged_content").
			Set("flagged_groups = EXCLUDED.flagged_groups").
			Set("confidence = EXCLUDED.confidence").
			Set("last_updated = EXCLUDED.last_updated").
			Set("thumbnail_url = EXCLUDED.thumbnail_url").
			Set("upvotes = EXCLUDED.upvotes").
			Set("downvotes = EXCLUDED.downvotes").
			Set("reputation = EXCLUDED.reputation").
			Exec(ctx)
		if err != nil {
			r.logger.Error("Error saving flagged user",
				zap.Uint64("userID", flaggedUser.ID),
				zap.String("username", flaggedUser.Name),
				zap.String("reason", flaggedUser.Reason),
				zap.Float64("confidence", flaggedUser.Confidence),
				zap.Error(err))
			continue
		}

		r.logger.Debug("Saved flagged user",
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

	r.logger.Debug("Finished saving flagged users")
}

// ConfirmUser moves a user from flagged_users to confirmed_users.
// This happens when a moderator confirms that a user is inappropriate.
// The user's groups and friends are tracked to help identify related users.
func (r *UserModel) ConfirmUser(ctx context.Context, user *FlaggedUser) error {
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		confirmedUser := &ConfirmedUser{
			User:       user.User,
			VerifiedAt: time.Now(),
		}

		_, err := tx.NewInsert().Model(confirmedUser).
			On("CONFLICT (id) DO UPDATE").
			Set("name = EXCLUDED.name").
			Set("display_name = EXCLUDED.display_name").
			Set("description = EXCLUDED.description").
			Set("created_at = EXCLUDED.created_at").
			Set("reason = EXCLUDED.reason").
			Set("groups = EXCLUDED.groups").
			Set("outfits = EXCLUDED.outfits").
			Set("friends = EXCLUDED.friends").
			Set("games = EXCLUDED.games").
			Set("flagged_content = EXCLUDED.flagged_content").
			Set("flagged_groups = EXCLUDED.flagged_groups").
			Set("confidence = EXCLUDED.confidence").
			Set("last_scanned = EXCLUDED.last_scanned").
			Set("last_updated = EXCLUDED.last_updated").
			Set("last_viewed = EXCLUDED.last_viewed").
			Set("last_purge_check = EXCLUDED.last_purge_check").
			Set("thumbnail_url = EXCLUDED.thumbnail_url").
			Set("upvotes = EXCLUDED.upvotes").
			Set("downvotes = EXCLUDED.downvotes").
			Set("reputation = EXCLUDED.reputation").
			Set("verified_at = EXCLUDED.verified_at").
			Exec(ctx)
		if err != nil {
			r.logger.Error("Failed to insert or update user in confirmed_users", zap.Error(err), zap.Uint64("userID", user.ID))
			return err
		}

		_, err = tx.NewDelete().Model((*FlaggedUser)(nil)).Where("id = ?", user.ID).Exec(ctx)
		if err != nil {
			r.logger.Error("Failed to delete user from flagged_users", zap.Error(err), zap.Uint64("userID", user.ID))
			return err
		}

		return nil
	})
}

// ClearUser moves a user from flagged_users to cleared_users.
// This happens when a moderator determines that a user was incorrectly flagged.
func (r *UserModel) ClearUser(ctx context.Context, user *FlaggedUser) error {
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		clearedUser := &ClearedUser{
			User:      user.User,
			ClearedAt: time.Now(),
		}

		_, err := tx.NewInsert().Model(clearedUser).
			On("CONFLICT (id) DO UPDATE").
			Set("name = EXCLUDED.name").
			Set("display_name = EXCLUDED.display_name").
			Set("description = EXCLUDED.description").
			Set("created_at = EXCLUDED.created_at").
			Set("reason = EXCLUDED.reason").
			Set("groups = EXCLUDED.groups").
			Set("outfits = EXCLUDED.outfits").
			Set("friends = EXCLUDED.friends").
			Set("games = EXCLUDED.games").
			Set("flagged_content = EXCLUDED.flagged_content").
			Set("flagged_groups = EXCLUDED.flagged_groups").
			Set("confidence = EXCLUDED.confidence").
			Set("last_scanned = EXCLUDED.last_scanned").
			Set("last_updated = EXCLUDED.last_updated").
			Set("last_viewed = EXCLUDED.last_viewed").
			Set("last_purge_check = EXCLUDED.last_purge_check").
			Set("thumbnail_url = EXCLUDED.thumbnail_url").
			Set("upvotes = EXCLUDED.upvotes").
			Set("downvotes = EXCLUDED.downvotes").
			Set("reputation = EXCLUDED.reputation").
			Set("cleared_at = EXCLUDED.cleared_at").
			Exec(ctx)
		if err != nil {
			r.logger.Error("Failed to insert or update user in cleared_users", zap.Error(err), zap.Uint64("userID", user.ID))
			return err
		}

		_, err = tx.NewDelete().Model((*FlaggedUser)(nil)).Where("id = ?", user.ID).Exec(ctx)
		if err != nil {
			r.logger.Error("Failed to delete user from flagged_users", zap.Error(err), zap.Uint64("userID", user.ID))
			return err
		}

		r.logger.Debug("User cleared and moved to cleared_users", zap.Uint64("userID", user.ID))

		return nil
	})
}

// GetFlaggedUserByIDToReview finds a user in the flagged_users table by their ID
// and updates their last_viewed timestamp.
func (r *UserModel) GetFlaggedUserByIDToReview(ctx context.Context, id uint64) (*FlaggedUser, error) {
	var user FlaggedUser
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Get the user with row lock
		err := tx.NewSelect().
			Model(&user).
			Where("id = ?", id).
			For("UPDATE SKIP LOCKED").
			Scan(ctx)
		if err != nil {
			r.logger.Error("Failed to get flagged user by ID",
				zap.Error(err),
				zap.Uint64("userID", id))
			return err
		}

		// Update last_viewed
		now := time.Now()
		_, err = tx.NewUpdate().
			Model(&user).
			Set("last_viewed = ?", now).
			Where("id = ?", id).
			Exec(ctx)
		if err != nil {
			r.logger.Error("Failed to update last_viewed",
				zap.Error(err),
				zap.Uint64("userID", id))
			return err
		}
		user.LastViewed = now

		return nil
	})
	if err != nil {
		return nil, err
	}

	r.logger.Debug("Retrieved and updated flagged user by ID",
		zap.Uint64("userID", id),
		zap.Time("lastViewed", user.LastViewed))
	return &user, nil
}

// GetClearedUserByID finds a user in the cleared_users table by their ID.
func (r *UserModel) GetClearedUserByID(ctx context.Context, id uint64) (*ClearedUser, error) {
	var user ClearedUser
	err := r.db.NewSelect().
		Model(&user).
		Where("id = ?", id).
		Scan(ctx)
	if err != nil {
		r.logger.Error("Failed to get cleared user by ID", zap.Error(err), zap.Uint64("userID", id))
		return nil, err
	}
	r.logger.Debug("Retrieved cleared user by ID", zap.Uint64("userID", id))
	return &user, nil
}

// GetConfirmedUsersCount returns the total number of users in confirmed_users.
func (r *UserModel) GetConfirmedUsersCount(ctx context.Context) (int, error) {
	count, err := r.db.NewSelect().
		Model((*ConfirmedUser)(nil)).
		Count(ctx)
	if err != nil {
		r.logger.Error("Failed to get confirmed users count", zap.Error(err))
		return 0, err
	}
	return count, nil
}

// GetFlaggedUsersCount returns the total number of users in flagged_users.
func (r *UserModel) GetFlaggedUsersCount(ctx context.Context) (int, error) {
	count, err := r.db.NewSelect().
		Model((*FlaggedUser)(nil)).
		Count(ctx)
	if err != nil {
		r.logger.Error("Failed to get flagged users count", zap.Error(err))
		return 0, err
	}
	return count, nil
}

// GetClearedUsersCount returns the total number of users in cleared_users.
func (r *UserModel) GetClearedUsersCount(ctx context.Context) (int, error) {
	count, err := r.db.NewSelect().
		Model((*ClearedUser)(nil)).
		Count(ctx)
	if err != nil {
		r.logger.Error("Failed to get cleared users count", zap.Error(err))
		return 0, err
	}
	return count, nil
}

// CheckExistingUsers finds which users from a list of IDs exist in any user table.
// Returns a map of user IDs to their status (confirmed, flagged, cleared).
func (r *UserModel) CheckExistingUsers(ctx context.Context, userIDs []uint64) (map[uint64]string, error) {
	var users []struct {
		ID     uint64
		Status string
	}

	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		err := tx.NewSelect().Model((*ConfirmedUser)(nil)).
			Column("id").
			ColumnExpr("? AS status", UserTypeConfirmed).
			Where("id IN (?)", bun.In(userIDs)).
			Union(
				tx.NewSelect().Model((*FlaggedUser)(nil)).
					Column("id").
					ColumnExpr("? AS status", UserTypeFlagged).
					Where("id IN (?)", bun.In(userIDs)),
			).
			Union(
				tx.NewSelect().Model((*ClearedUser)(nil)).
					Column("id").
					ColumnExpr("? AS status", UserTypeCleared).
					Where("id IN (?)", bun.In(userIDs)),
			).
			Scan(ctx, &users)
		return err
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

// GetUsersByIDs retrieves full user information for a list of user IDs.
// Returns a map of user IDs to user data and a separate map for their types.
func (r *UserModel) GetUsersByIDs(ctx context.Context, userIDs []uint64) (map[uint64]*User, map[uint64]string, error) {
	users := make(map[uint64]*User)
	userTypes := make(map[uint64]string)

	// Query confirmed users
	var confirmedUsers []ConfirmedUser
	err := r.db.NewSelect().
		Model(&confirmedUsers).
		Where("id IN (?)", bun.In(userIDs)).
		Scan(ctx)
	if err != nil {
		return nil, nil, err
	}
	for _, user := range confirmedUsers {
		users[user.ID] = &user.User
		userTypes[user.ID] = UserTypeConfirmed
	}

	// Query flagged users
	var flaggedUsers []FlaggedUser
	err = r.db.NewSelect().
		Model(&flaggedUsers).
		Where("id IN (?)", bun.In(userIDs)).
		Scan(ctx)
	if err != nil {
		return nil, nil, err
	}
	for _, user := range flaggedUsers {
		users[user.ID] = &user.User
		userTypes[user.ID] = UserTypeFlagged
	}

	r.logger.Debug("Retrieved users by IDs",
		zap.Int("requestedCount", len(userIDs)),
		zap.Int("foundCount", len(users)))

	return users, userTypes, nil
}

// GetUsersToCheck finds users that haven't been checked for banned status recently.
// Returns a batch of user IDs and updates their last_purge_check timestamp.
func (r *UserModel) GetUsersToCheck(ctx context.Context, limit int) ([]uint64, error) {
	var userIDs []uint64

	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Use CTE to select users
		err := tx.NewRaw(`
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
		`, limit/2, limit/2).Scan(ctx, &userIDs)
		if err != nil {
			r.logger.Error("Failed to get users to check", zap.Error(err))
			return err
		}

		// Update last_purge_check for selected users
		if len(userIDs) > 0 {
			_, err = tx.NewUpdate().Model((*ConfirmedUser)(nil)).
				Set("last_purge_check = ?", time.Now()).
				Where("id IN (?)", bun.In(userIDs)).
				Exec(ctx)
			if err != nil {
				r.logger.Error("Failed to update last_purge_check for confirmed users", zap.Error(err))
				return err
			}

			_, err = tx.NewUpdate().Model((*FlaggedUser)(nil)).
				Set("last_purge_check = ?", time.Now()).
				Where("id IN (?)", bun.In(userIDs)).
				Exec(ctx)
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

	r.logger.Debug("Retrieved and updated users to check", zap.Int("count", len(userIDs)))
	return userIDs, nil
}

// RemoveBannedUsers moves users from confirmed_users and flagged_users to banned_users.
// This happens when users are found to be banned by Roblox.
func (r *UserModel) RemoveBannedUsers(ctx context.Context, userIDs []uint64) error {
	return r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Move confirmed users to banned_users
		var confirmedUsers []ConfirmedUser
		err := tx.NewSelect().Model(&confirmedUsers).
			Where("id IN (?)", bun.In(userIDs)).
			Scan(ctx)
		if err != nil {
			r.logger.Error("Failed to select confirmed users for banning", zap.Error(err))
			return err
		}

		for _, user := range confirmedUsers {
			bannedUser := &BannedUser{
				User:     user.User,
				PurgedAt: time.Now(),
			}
			_, err = tx.NewInsert().Model(bannedUser).
				On("CONFLICT (id) DO UPDATE").
				Exec(ctx)
			if err != nil {
				r.logger.Error("Failed to insert banned user from confirmed_users", zap.Error(err), zap.Uint64("userID", user.ID))
				return err
			}
		}

		// Move flagged users to banned_users
		var flaggedUsers []FlaggedUser
		err = tx.NewSelect().Model(&flaggedUsers).
			Where("id IN (?)", bun.In(userIDs)).
			Scan(ctx)
		if err != nil {
			r.logger.Error("Failed to select flagged users for banning", zap.Error(err))
			return err
		}

		for _, user := range flaggedUsers {
			bannedUser := &BannedUser{
				User:     user.User,
				PurgedAt: time.Now(),
			}
			_, err = tx.NewInsert().Model(bannedUser).
				On("CONFLICT (id) DO UPDATE").
				Exec(ctx)
			if err != nil {
				r.logger.Error("Failed to insert banned user from flagged_users", zap.Error(err), zap.Uint64("userID", user.ID))
				return err
			}
		}

		// Remove users from confirmed_users
		_, err = tx.NewDelete().Model((*ConfirmedUser)(nil)).
			Where("id IN (?)", bun.In(userIDs)).
			Exec(ctx)
		if err != nil {
			r.logger.Error("Failed to remove banned users from confirmed_users", zap.Error(err))
			return err
		}

		// Remove users from flagged_users
		_, err = tx.NewDelete().Model((*FlaggedUser)(nil)).
			Where("id IN (?)", bun.In(userIDs)).
			Exec(ctx)
		if err != nil {
			r.logger.Error("Failed to remove banned users from flagged_users", zap.Error(err))
			return err
		}

		r.logger.Debug("Moved banned users to banned_users", zap.Int("count", len(userIDs)))
		return nil
	})
}

// PurgeOldClearedUsers removes cleared users older than the cutoff date.
// This helps maintain database size by removing users that were cleared long ago.
func (r *UserModel) PurgeOldClearedUsers(ctx context.Context, cutoffDate time.Time) (int, error) {
	result, err := r.db.NewDelete().
		Model((*ClearedUser)(nil)).
		Where("cleared_at < ?", cutoffDate).
		Exec(ctx)
	if err != nil {
		r.logger.Error("Failed to purge old cleared users",
			zap.Error(err),
			zap.Time("cutoffDate", cutoffDate))
		return 0, err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		r.logger.Error("Failed to get rows affected", zap.Error(err))
		return 0, err
	}

	r.logger.Debug("Purged old cleared users",
		zap.Int64("rowsAffected", affected),
		zap.Time("cutoffDate", cutoffDate))

	return int(affected), nil
}

// UpdateTrainingVotes updates the upvotes or downvotes count for a user in training mode.
func (r *UserModel) UpdateTrainingVotes(ctx context.Context, userID uint64, isUpvote bool) error {
	err := r.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// First get current vote counts
		var user FlaggedUser
		err := tx.NewSelect().
			Model(&user).
			Column("upvotes", "downvotes").
			Where("id = ?", userID).
			Scan(ctx)
		if err != nil {
			return err
		}

		// Increment appropriate vote count
		if isUpvote {
			user.Upvotes++
		} else {
			user.Downvotes++
		}

		// Calculate new reputation
		newReputation := user.Upvotes - user.Downvotes

		// Update the user with new values
		_, err = tx.NewUpdate().
			Model((*FlaggedUser)(nil)).
			Set("upvotes = ?", user.Upvotes).
			Set("downvotes = ?", user.Downvotes).
			Set("reputation = ?", newReputation).
			Where("id = ?", userID).
			Exec(ctx)
		if err != nil {
			r.logger.Error("Failed to update training votes",
				zap.Error(err),
				zap.Uint64("userID", userID),
				zap.String("voteType", map[bool]string{true: "upvote", false: "downvote"}[isUpvote]))
			return err
		}

		return nil
	})

	return err
}
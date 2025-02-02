package types

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
)

var (
	ErrUserNotFound     = errors.New("user not found")
	ErrInvalidUserID    = errors.New("invalid user ID")
	ErrNoUsersToReview  = errors.New("no users available to review")
	ErrUnsupportedModel = errors.New("unsupported model type")
)

// User combines all the information needed to review a user.
// This base structure is embedded in other user types (Flagged, Confirmed).
type User struct {
	ID                  uint64                  `bun:",pk"        json:"id"`
	UUID                uuid.UUID               `bun:",notnull"   json:"uuid"`
	Name                string                  `bun:",notnull"   json:"name"`
	DisplayName         string                  `bun:",notnull"   json:"displayName"`
	Description         string                  `bun:",notnull"   json:"description"`
	CreatedAt           time.Time               `bun:",notnull"   json:"createdAt"`
	Reason              string                  `bun:",notnull"   json:"reason"`
	Groups              []*types.UserGroupRoles `bun:"type:jsonb" json:"groups"`
	Outfits             []*types.Outfit         `bun:"type:jsonb" json:"outfits"`
	Friends             []*types.ExtendedFriend `bun:"type:jsonb" json:"friends"`
	Games               []*types.Game           `bun:"type:jsonb" json:"games"`
	FlaggedContent      []string                `bun:"type:jsonb" json:"flaggedContent"`
	FollowerCount       uint64                  `bun:",notnull"   json:"followerCount"`
	FollowingCount      uint64                  `bun:",notnull"   json:"followingCount"`
	Confidence          float64                 `bun:",notnull"   json:"confidence"`
	LastScanned         time.Time               `bun:",notnull"   json:"lastScanned"`
	LastUpdated         time.Time               `bun:",notnull"   json:"lastUpdated"`
	LastViewed          time.Time               `bun:",notnull"   json:"lastViewed"`
	LastBanCheck        time.Time               `bun:",notnull"   json:"lastBanCheck"`
	IsBanned            bool                    `bun:",notnull"   json:"isBanned"`
	ThumbnailURL        string                  `bun:",notnull"   json:"thumbnailUrl"`
	LastThumbnailUpdate time.Time               `bun:",notnull"   json:"lastThumbnailUpdate"`
}

// FlaggedUser extends User to track users that need review.
// The base User structure contains all the fields needed for review.
type FlaggedUser struct {
	User `json:"user"`
}

// ConfirmedUser extends User to track users that have been reviewed and confirmed.
// The VerifiedAt field shows when the user was confirmed by a moderator.
type ConfirmedUser struct {
	User       `json:"user"`
	VerifiedAt time.Time `bun:",notnull" json:"verifiedAt"`
}

// ClearedUser extends User to track users that were cleared during review.
// The ClearedAt field shows when the user was cleared by a moderator.
type ClearedUser struct {
	User      `json:"user"`
	ClearedAt time.Time `bun:",notnull" json:"clearedAt"`
}

// ReviewUser combines all possible user states into a single structure for review.
type ReviewUser struct {
	User       `json:"user"`
	VerifiedAt time.Time     `json:"verifiedAt,omitempty"`
	ClearedAt  time.Time     `json:"clearedAt,omitempty"`
	Status     enum.UserType `json:"status"`
	Reputation *Reputation   `json:"reputation"`
}

// UserField represents available fields as bit flags.
type UserField uint32

const (
	UserFieldNone UserField = 0

	// Basic user information
	UserFieldID          UserField = 1 << iota // User ID
	UserFieldName                              // Username
	UserFieldDisplayName                       // Display name
	UserFieldDescription                       // User description
	UserFieldCreatedAt                         // Account creation date
	UserFieldReason                            // Reason for flagging
	UserFieldThumbnail                         // ThumbnailURL

	// Relationships and content
	UserFieldGroups         // Group memberships
	UserFieldOutfits        // User outfits
	UserFieldFriends        // Friend list
	UserFieldGames          // Played games
	UserFieldFlaggedContent // Flagged content

	// Statistics
	UserFieldFollowerCount  // Follower count
	UserFieldFollowingCount // Following count
	UserFieldConfidence     // AI confidence score
	UserFieldUpvotes        // Reputation upvotes
	UserFieldDownvotes      // Reputation downvotes
	UserFieldScore          // Reputation score

	// Timestamps
	UserFieldLastScanned         // Last scan time
	UserFieldLastUpdated         // Last update time
	UserFieldLastViewed          // Last view time
	UserFieldLastBanCheck        // Last ban check time
	UserFieldIsBanned            // Ban status
	UserFieldLastThumbnailUpdate // Last thumbnail update

	// Common combinations
	// UserFieldBasic includes the essential user identification fields:
	// ID, username, and display name
	UserFieldBasic = UserFieldID |
		UserFieldName |
		UserFieldDisplayName

	// UserFieldProfile includes all profile-related fields:
	// description, creation date, and thumbnail
	UserFieldProfile = UserFieldDescription |
		UserFieldCreatedAt |
		UserFieldThumbnail

	// UserFieldRelationships includes all relationship-related fields:
	// groups, outfits, friends, and games
	UserFieldRelationships = UserFieldGroups |
		UserFieldOutfits |
		UserFieldFriends |
		UserFieldGames

	// UserFieldStats includes all statistical fields:
	// follower/following counts and confidence score
	UserFieldStats = UserFieldFollowerCount |
		UserFieldFollowingCount |
		UserFieldConfidence

	// UserFieldReputation includes all reputation-related fields:
	// upvotes, downvotes, and overall score
	UserFieldReputation = UserFieldUpvotes |
		UserFieldDownvotes |
		UserFieldScore

	// UserFieldTimestamps includes all timestamp-related fields:
	// last scanned, updated, viewed, ban check, ban status, and thumbnail update
	UserFieldTimestamps = UserFieldLastScanned |
		UserFieldLastUpdated |
		UserFieldLastViewed |
		UserFieldLastBanCheck |
		UserFieldIsBanned |
		UserFieldLastThumbnailUpdate

	// UserFieldAll includes all available fields
	UserFieldAll = UserFieldBasic |
		UserFieldProfile |
		UserFieldRelationships |
		UserFieldStats |
		UserFieldReputation |
		UserFieldTimestamps |
		UserFieldFlaggedContent
)

// userFieldToColumns maps UserField bits to their corresponding database columns.
var userFieldToColumns = map[UserField][]string{ //nolint:gochecknoglobals
	UserFieldID:                  {"id"},
	UserFieldName:                {"name"},
	UserFieldDisplayName:         {"display_name"},
	UserFieldDescription:         {"description"},
	UserFieldCreatedAt:           {"created_at"},
	UserFieldReason:              {"reason"},
	UserFieldThumbnail:           {"thumbnail_url"},
	UserFieldGroups:              {"groups"},
	UserFieldOutfits:             {"outfits"},
	UserFieldFriends:             {"friends"},
	UserFieldGames:               {"games"},
	UserFieldFlaggedContent:      {"flagged_content"},
	UserFieldFollowerCount:       {"follower_count"},
	UserFieldFollowingCount:      {"following_count"},
	UserFieldConfidence:          {"confidence"},
	UserFieldUpvotes:             {"upvotes"},
	UserFieldDownvotes:           {"downvotes"},
	UserFieldScore:               {"score"},
	UserFieldLastScanned:         {"last_scanned"},
	UserFieldLastUpdated:         {"last_updated"},
	UserFieldLastViewed:          {"last_viewed"},
	UserFieldLastBanCheck:        {"last_ban_check"},
	UserFieldIsBanned:            {"is_banned"},
	UserFieldLastThumbnailUpdate: {"last_thumbnail_update"},
}

// Columns returns the list of database columns to fetch based on the selected fields.
func (f UserField) Columns() []string {
	if f == UserFieldNone {
		return []string{"*"}
	}

	var columns []string
	for field, cols := range userFieldToColumns {
		if f&field != 0 {
			columns = append(columns, cols...)
		}
	}
	return columns
}

// Add adds the specified fields to the current selection.
func (f UserField) Add(fields UserField) UserField {
	return f | fields
}

// Remove removes the specified fields from the current selection.
func (f UserField) Remove(fields UserField) UserField {
	return f &^ fields
}

// Has checks if all specified fields are included.
func (f UserField) Has(fields UserField) bool {
	return f&fields == fields
}

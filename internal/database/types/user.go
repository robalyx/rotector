package types

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/robalyx/rotector/internal/database/types/enum"
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
	ID                  uint64                       `bun:",pk"                    json:"id"`
	UUID                uuid.UUID                    `bun:",notnull"               json:"uuid"`
	Name                string                       `bun:",notnull"               json:"name"`
	DisplayName         string                       `bun:",notnull"               json:"displayName"`
	Description         string                       `bun:",notnull"               json:"description"`
	CreatedAt           time.Time                    `bun:",notnull"               json:"createdAt"`
	Reasons             Reasons[enum.UserReasonType] `bun:"type:jsonb"             json:"reasons"`
	Groups              []*types.UserGroupRoles      `bun:"type:jsonb"             json:"groups"`
	Outfits             []*types.Outfit              `bun:"type:jsonb"             json:"outfits"`
	Friends             []*types.ExtendedFriend      `bun:"type:jsonb"             json:"friends"`
	Games               []*types.Game                `bun:"type:jsonb"             json:"games"`
	Inventory           []*types.InventoryAsset      `bun:"type:jsonb"             json:"inventory"`
	Confidence          float64                      `bun:",notnull"               json:"confidence"`
	HasSocials          bool                         `bun:",notnull,default:false" json:"hasSocials"`
	LastScanned         time.Time                    `bun:",notnull"               json:"lastScanned"`
	LastUpdated         time.Time                    `bun:",notnull"               json:"lastUpdated"`
	LastViewed          time.Time                    `bun:",notnull"               json:"lastViewed"`
	LastBanCheck        time.Time                    `bun:",notnull"               json:"lastBanCheck"`
	IsBanned            bool                         `bun:",notnull,default:false" json:"isBanned"`
	IsDeleted           bool                         `bun:",notnull,default:false" json:"isDeleted"`
	ThumbnailURL        string                       `bun:",notnull"               json:"thumbnailUrl"`
	LastThumbnailUpdate time.Time                    `bun:",notnull"               json:"lastThumbnailUpdate"`
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
	VerifiedAt time.Time     `json:"verifiedAt"`
	ClearedAt  time.Time     `json:"clearedAt"`
	Status     enum.UserType `json:"status"`
	Reputation Reputation    `json:"reputation"`
}

// UserField represents available fields as bit flags.
type UserField uint32

const (
	UserFieldNone UserField = 0

	UserFieldID          UserField = 1 << iota // User ID
	UserFieldUUID                              // User UUID
	UserFieldName                              // Username
	UserFieldDisplayName                       // Display name
	UserFieldDescription                       // User description
	UserFieldCreatedAt                         // Account creation date
	UserFieldReasons                           // Reasons for flagging
	UserFieldThumbnail                         // ThumbnailURL
	UserFieldHasSocials                        // Has social media links

	UserFieldGroups  // Group memberships
	UserFieldOutfits // User outfits
	UserFieldFriends // Friend list
	UserFieldGames   // Played games

	UserFieldConfidence // AI confidence score

	UserFieldReputation // Reputation fields (upvotes, downvotes, score)

	UserFieldLastScanned         // Last scan time
	UserFieldLastUpdated         // Last update time
	UserFieldLastViewed          // Last view time
	UserFieldLastBanCheck        // Last ban check time
	UserFieldIsBanned            // Ban status
	UserFieldIsDeleted           // Deletion status
	UserFieldLastThumbnailUpdate // Last thumbnail update

	// UserFieldBasic includes all basic fields.
	UserFieldBasic = UserFieldID |
		UserFieldUUID |
		UserFieldName |
		UserFieldDisplayName

	// UserFieldProfile includes all profile-related fields.
	UserFieldProfile = UserFieldDescription |
		UserFieldCreatedAt |
		UserFieldThumbnail |
		UserFieldHasSocials |
		UserFieldIsBanned |
		UserFieldIsDeleted

	// UserFieldRelationships includes all relationship-related fields.
	UserFieldRelationships = UserFieldGroups |
		UserFieldOutfits |
		UserFieldFriends |
		UserFieldGames

	// UserFieldStats includes all statistical fields.
	UserFieldStats = UserFieldConfidence

	// UserFieldTimestamps includes all timestamp-related fields.
	UserFieldTimestamps = UserFieldLastScanned |
		UserFieldLastUpdated |
		UserFieldLastViewed |
		UserFieldLastBanCheck |
		UserFieldLastThumbnailUpdate

	// UserFieldAll includes all available fields.
	UserFieldAll = UserFieldBasic |
		UserFieldProfile |
		UserFieldReasons |
		UserFieldRelationships |
		UserFieldStats |
		UserFieldReputation |
		UserFieldTimestamps
)

// userFieldToColumns maps UserField bits to their corresponding database columns.
var userFieldToColumns = map[UserField][]string{
	UserFieldID:                  {"id"},
	UserFieldUUID:                {"uuid"},
	UserFieldName:                {"name"},
	UserFieldDisplayName:         {"display_name"},
	UserFieldDescription:         {"description"},
	UserFieldCreatedAt:           {"created_at"},
	UserFieldReasons:             {"reasons"},
	UserFieldThumbnail:           {"thumbnail_url"},
	UserFieldGroups:              {"groups"},
	UserFieldOutfits:             {"outfits"},
	UserFieldFriends:             {"friends"},
	UserFieldGames:               {"games"},
	UserFieldConfidence:          {"confidence"},
	UserFieldLastScanned:         {"last_scanned"},
	UserFieldLastUpdated:         {"last_updated"},
	UserFieldLastViewed:          {"last_viewed"},
	UserFieldLastBanCheck:        {"last_ban_check"},
	UserFieldIsBanned:            {"is_banned"},
	UserFieldIsDeleted:           {"is_deleted"},
	UserFieldLastThumbnailUpdate: {"last_thumbnail_update"},
}

// HasReputation returns true if the reputation fields should be included.
func (f UserField) HasReputation() bool {
	return f&UserFieldReputation != 0
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

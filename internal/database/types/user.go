package types

import (
	"errors"
	"time"

	"github.com/google/uuid"
	apiTypes "github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/robalyx/rotector/internal/database/types/enum"
)

var (
	ErrUserNotFound     = errors.New("user not found")
	ErrInvalidUserID    = errors.New("invalid user ID")
	ErrNoUsersToReview  = errors.New("no users available to review")
	ErrUnsupportedModel = errors.New("unsupported model type")
)

// User represents a user in any state (flagged, confirmed, or cleared).
type User struct {
	ID                  uint64        `bun:",pk"                    json:"id"`
	UUID                uuid.UUID     `bun:",notnull"               json:"uuid"`
	Name                string        `bun:",notnull"               json:"name"`
	DisplayName         string        `bun:",notnull"               json:"displayName"`
	Description         string        `bun:",notnull"               json:"description"`
	CreatedAt           time.Time     `bun:",notnull"               json:"createdAt"`
	Status              enum.UserType `bun:",notnull"               json:"status"`
	Confidence          float64       `bun:",notnull"               json:"confidence"`
	HasSocials          bool          `bun:",notnull,default:false" json:"hasSocials"`
	LastScanned         time.Time     `bun:",notnull"               json:"lastScanned"`
	LastUpdated         time.Time     `bun:",notnull"               json:"lastUpdated"`
	LastViewed          time.Time     `bun:",notnull"               json:"lastViewed"`
	LastBanCheck        time.Time     `bun:",notnull"               json:"lastBanCheck"`
	IsBanned            bool          `bun:",notnull,default:false" json:"isBanned"`
	IsDeleted           bool          `bun:",notnull,default:false" json:"isDeleted"`
	ThumbnailURL        string        `bun:",notnull"               json:"thumbnailUrl"`
	LastThumbnailUpdate time.Time     `bun:",notnull"               json:"lastThumbnailUpdate"`
}

// UserReason represents a reason for flagging a user.
type UserReason struct {
	UserID     uint64              `bun:",pk"      json:"userId"`
	ReasonType enum.UserReasonType `bun:",pk"      json:"reasonType"`
	Message    string              `bun:",notnull" json:"message"`
	Confidence float64             `bun:",notnull" json:"confidence"`
	Evidence   []string            `bun:",notnull" json:"evidence"`
	CreatedAt  time.Time           `bun:",notnull" json:"createdAt"`
}

// UserGroup represents a user's group membership.
type UserGroup struct {
	UserID   uint64 `bun:",pk"      json:"userId"`
	GroupID  uint64 `bun:",pk"      json:"groupId"`
	RoleID   uint64 `bun:",notnull" json:"roleId"`
	RoleName string `bun:",notnull" json:"roleName"`
	RoleRank uint64 `bun:",notnull" json:"roleRank"`

	Group *GroupInfo `bun:"rel:belongs-to,join:group_id=id" json:"-"`
}

// GroupInfo stores the shared group information.
type GroupInfo struct {
	ID                 uint64               `bun:",pk"       json:"id"`
	Name               string               `bun:",notnull"  json:"name"`
	Description        string               `bun:",notnull"  json:"description"`
	Owner              *apiTypes.GroupUser  `bun:",nullzero" json:"owner"`
	Shout              *apiTypes.GroupShout `bun:",nullzero" json:"shout"`
	MemberCount        uint64               `bun:",notnull"  json:"memberCount"`
	HasVerifiedBadge   bool                 `bun:",notnull"  json:"hasVerifiedBadge"`
	IsBuildersClubOnly bool                 `bun:",notnull"  json:"isBuildersClubOnly"`
	PublicEntryAllowed bool                 `bun:",notnull"  json:"publicEntryAllowed"`
	IsLocked           bool                 `bun:",notnull"  json:"isLocked"`
	LastUpdated        time.Time            `bun:",notnull"  json:"lastUpdated"`
}

// UserOutfit represents a user's outfit.
type UserOutfit struct {
	UserID   uint64 `bun:",pk" json:"userId"`
	OutfitID uint64 `bun:",pk" json:"outfitId"`

	Outfit *OutfitInfo `bun:"rel:belongs-to,join:outfit_id=id" json:"-"`
}

// OutfitInfo stores the shared outfit information.
type OutfitInfo struct {
	ID          uint64    `bun:",pk"      json:"id"`
	Name        string    `bun:",notnull" json:"name"`
	IsEditable  bool      `bun:",notnull" json:"isEditable"`
	OutfitType  string    `bun:",notnull" json:"outfitType"`
	LastUpdated time.Time `bun:",notnull" json:"lastUpdated"`

	OutfitAssets []*OutfitAsset `bun:"rel:has-many,join:id=outfit_id" json:"-"`
}

// OutfitAsset represents an outfit's asset.
type OutfitAsset struct {
	OutfitID         uint64 `bun:",pk"      json:"outfitId"`
	AssetID          uint64 `bun:",pk"      json:"assetId"`
	CurrentVersionID uint64 `bun:",notnull" json:"currentVersionId"`

	Asset *AssetInfo `bun:"rel:belongs-to,join:asset_id=id" json:"-"`
}

// AssetInfo stores the shared asset information.
type AssetInfo struct {
	ID          uint64                 `bun:",pk"      json:"id"`
	Name        string                 `bun:",notnull" json:"name"`
	AssetType   apiTypes.ItemAssetType `bun:",notnull" json:"assetType"`
	LastUpdated time.Time              `bun:",notnull" json:"lastUpdated"`
}

// UserFriend represents a user's friend.
type UserFriend struct {
	UserID   uint64 `bun:",pk" json:"userId"`
	FriendID uint64 `bun:",pk" json:"friendId"`

	Friend *FriendInfo `bun:"rel:belongs-to,join:friend_id=id" json:"-"`
}

// FriendInfo stores the shared friend information.
type FriendInfo struct {
	ID          uint64    `bun:",pk"      json:"id"`
	Name        string    `bun:",notnull" json:"name"`
	DisplayName string    `bun:",notnull" json:"displayName"`
	LastUpdated time.Time `bun:",notnull" json:"lastUpdated"`
}

// UserGame represents a user's game.
type UserGame struct {
	UserID uint64 `bun:",pk" json:"userId"`
	GameID uint64 `bun:",pk" json:"gameId"`

	Game *GameInfo `bun:"rel:belongs-to,join:game_id=id" json:"-"`
}

// GameInfo stores the shared game information.
type GameInfo struct {
	ID          uint64    `bun:",pk"      json:"id"`
	Name        string    `bun:",notnull" json:"name"`
	Description string    `bun:",notnull" json:"description"`
	PlaceVisits uint64    `bun:",notnull" json:"placeVisits"`
	Created     time.Time `bun:",notnull" json:"created"`
	Updated     time.Time `bun:",notnull" json:"updated"`
	LastUpdated time.Time `bun:",notnull" json:"lastUpdated"`
}

// UserInventory represents a user's inventory item.
type UserInventory struct {
	UserID      uint64 `bun:",pk" json:"userId"`
	InventoryID uint64 `bun:",pk" json:"inventoryId"`

	Inventory *InventoryInfo `bun:"rel:belongs-to,join:inventory_id=id" json:"-"`
}

// InventoryInfo stores the shared inventory item information.
type InventoryInfo struct {
	ID          uint64    `bun:",pk"      json:"id"`
	Name        string    `bun:",notnull" json:"name"`
	AssetType   string    `bun:",notnull" json:"assetType"`
	Created     time.Time `bun:",notnull" json:"created"`
	LastUpdated time.Time `bun:",notnull" json:"lastUpdated"`
}

// UserFavorite represents a user's favorite item.
type UserFavorite struct {
	UserID uint64 `bun:",pk" json:"userId"`
	GameID uint64 `bun:",pk" json:"gameId"`

	Game *GameInfo `bun:"rel:belongs-to,join:game_id=id" json:"-"`
}

// UserBadge represents a user's badge.
type UserBadge struct {
	UserID  uint64 `bun:",pk"      json:"userId"`
	BadgeID uint64 `bun:",notnull" json:"badgeId"`
	Badge   any    `bun:",notnull" json:"badge"`
}

// UserVerification stores verification data for confirmed users.
type UserVerification struct {
	UserID     uint64    `bun:",pk"      json:"userId"`
	ReviewerID uint64    `bun:",notnull" json:"reviewerId"`
	VerifiedAt time.Time `bun:",notnull" json:"verifiedAt"`
}

// UserClearance stores clearance data for cleared users.
type UserClearance struct {
	UserID     uint64    `bun:",pk"      json:"userId"`
	ReviewerID uint64    `bun:",notnull" json:"reviewerId"`
	ClearedAt  time.Time `bun:",notnull" json:"clearedAt"`
}

// UserAsset represents a user's currently equipped asset.
type UserAsset struct {
	UserID           uint64 `bun:",pk"      json:"userId"`
	AssetID          uint64 `bun:",pk"      json:"assetId"`
	CurrentVersionID uint64 `bun:",notnull" json:"currentVersionId"`

	Asset *AssetInfo `bun:"rel:belongs-to,join:asset_id=id" json:"-"`
}

// ReviewUser combines user data with verification/clearance info for review.
type ReviewUser struct {
	*User
	Reasons       Reasons[enum.UserReasonType]   `json:"reasons"`
	ReviewerID    uint64                         `json:"reviewerId,omitempty"`
	VerifiedAt    time.Time                      `json:"verifiedAt"`
	ClearedAt     time.Time                      `json:"clearedAt"`
	Reputation    Reputation                     `json:"reputation"`
	Groups        []*apiTypes.UserGroupRoles     `json:"groups,omitempty"`
	Outfits       []*apiTypes.Outfit             `json:"outfits,omitempty"`
	OutfitAssets  map[uint64][]*apiTypes.AssetV2 `json:"outfitAssets,omitempty"`
	CurrentAssets []*apiTypes.AssetV2            `json:"currentAssets,omitempty"`
	Friends       []*apiTypes.ExtendedFriend     `json:"friends,omitempty"`
	Games         []*apiTypes.Game               `json:"games,omitempty"`
	Inventory     []*apiTypes.InventoryAsset     `json:"inventory,omitempty"`
	Favorites     []*apiTypes.Game               `json:"favorites,omitempty"`
	Badges        []any                          `json:"badges,omitempty"`
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
	UserFieldStatus                            // User status
	UserFieldReasons                           // Reasons for flagging
	UserFieldThumbnail                         // ThumbnailURL
	UserFieldHasSocials                        // Has social media links

	UserFieldRelationships // All relationships (groups, outfits, friends, games, inventory, etc.)

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
		UserFieldDisplayName |
		UserFieldStatus

	// UserFieldProfile includes all profile-related fields.
	UserFieldProfile = UserFieldDescription |
		UserFieldCreatedAt |
		UserFieldThumbnail |
		UserFieldHasSocials |
		UserFieldIsBanned |
		UserFieldIsDeleted

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
	UserFieldStatus:              {"status"},
	UserFieldThumbnail:           {"thumbnail_url"},
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

// UserGroupQueryResult combines user group and group info for database queries.
type UserGroupQueryResult struct {
	UserGroup
	Name               string               `bun:"name"                  json:"name"`
	Description        string               `bun:"description"           json:"description"`
	Owner              *apiTypes.GroupUser  `bun:"owner"                 json:"owner"`
	Shout              *apiTypes.GroupShout `bun:"shout"                 json:"shout"`
	MemberCount        uint64               `bun:"member_count"          json:"memberCount"`
	HasVerifiedBadge   bool                 `bun:"has_verified_badge"    json:"hasVerifiedBadge"`
	IsBuildersClubOnly bool                 `bun:"is_builders_club_only" json:"isBuildersClubOnly"`
	PublicEntryAllowed bool                 `bun:"public_entry_allowed"  json:"publicEntryAllowed"`
	IsLocked           bool                 `bun:"is_locked"             json:"isLocked"`
}

// ToAPIType converts a UserGroupQueryResult to an API UserGroupRoles type.
func (r *UserGroupQueryResult) ToAPIType() *apiTypes.UserGroupRoles {
	return &apiTypes.UserGroupRoles{
		Group: apiTypes.GroupResponse{
			ID:                 r.GroupID,
			Name:               r.Name,
			Description:        r.Description,
			Owner:              r.Owner,
			Shout:              r.Shout,
			MemberCount:        r.MemberCount,
			HasVerifiedBadge:   r.HasVerifiedBadge,
			IsBuildersClubOnly: r.IsBuildersClubOnly,
			PublicEntryAllowed: r.PublicEntryAllowed,
			IsLocked:           &r.IsLocked,
		},
		Role: apiTypes.UserGroupRole{
			ID:   r.RoleID,
			Name: r.RoleName,
			Rank: r.RoleRank,
		},
	}
}

// UserOutfitQueryResult combines user outfit and outfit info for database queries.
type UserOutfitQueryResult struct {
	UserOutfit
	Name       string `bun:"name"        json:"name"`
	IsEditable bool   `bun:"is_editable" json:"isEditable"`
	OutfitType string `bun:"outfit_type" json:"outfitType"`
}

// ToAPIType converts a UserOutfitQueryResult to an API Outfit type.
func (r *UserOutfitQueryResult) ToAPIType() *apiTypes.Outfit {
	return &apiTypes.Outfit{
		ID:         r.OutfitID,
		Name:       r.Name,
		IsEditable: r.IsEditable,
		OutfitType: r.OutfitType,
	}
}

// UserFriendQueryResult combines user friend and friend info for database queries.
type UserFriendQueryResult struct {
	UserFriend
	Name        string `bun:"name"         json:"name"`
	DisplayName string `bun:"display_name" json:"displayName"`
}

// ToAPIType converts a UserFriendQueryResult to an API ExtendedFriend type.
func (r *UserFriendQueryResult) ToAPIType() *apiTypes.ExtendedFriend {
	return &apiTypes.ExtendedFriend{
		Friend: apiTypes.Friend{
			ID: r.FriendID,
		},
		Name:        r.Name,
		DisplayName: r.DisplayName,
	}
}

// UserGameQueryResult combines user game and game info for database queries.
type UserGameQueryResult struct {
	UserGame
	Name        string    `bun:"name"         json:"name"`
	Description string    `bun:"description"  json:"description"`
	PlaceVisits uint64    `bun:"place_visits" json:"placeVisits"`
	Created     time.Time `bun:"created"      json:"created"`
	Updated     time.Time `bun:"updated"      json:"updated"`
}

// ToAPIType converts a UserGameQueryResult to an API Game type.
func (r *UserGameQueryResult) ToAPIType() *apiTypes.Game {
	return &apiTypes.Game{
		ID:          r.GameID,
		Name:        r.Name,
		Description: r.Description,
		PlaceVisits: r.PlaceVisits,
		Created:     r.Created,
		Updated:     r.Updated,
	}
}

// UserInventoryQueryResult combines user inventory and inventory info for database queries.
type UserInventoryQueryResult struct {
	UserInventory
	Name      string    `bun:"name"       json:"name"`
	AssetType string    `bun:"asset_type" json:"assetType"`
	Created   time.Time `bun:"created"    json:"created"`
}

// ToAPIType converts a UserInventoryQueryResult to an API InventoryAsset type.
func (r *UserInventoryQueryResult) ToAPIType() *apiTypes.InventoryAsset {
	return &apiTypes.InventoryAsset{
		AssetID:   r.InventoryID,
		Name:      r.Name,
		AssetType: r.AssetType,
		Created:   r.Created,
	}
}

// UserAssetQueryResult combines user asset and asset info for database queries.
type UserAssetQueryResult struct {
	UserAsset
	Name      string                 `bun:"name"       json:"name"`
	AssetType apiTypes.ItemAssetType `bun:"asset_type" json:"assetType"`
}

// ToAPIType converts a UserAssetQueryResult to an API AssetV2 type.
func (r *UserAssetQueryResult) ToAPIType() *apiTypes.AssetV2 {
	return &apiTypes.AssetV2{
		ID:   r.AssetID,
		Name: r.Name,
		AssetType: apiTypes.AssetType{
			ID: r.AssetType,
		},
		CurrentVersionID: r.CurrentVersionID,
	}
}

// UserFavoriteQueryResult combines user favorite and game info for database queries.
type UserFavoriteQueryResult struct {
	UserFavorite
	Name        string    `bun:"name"         json:"name"`
	Description string    `bun:"description"  json:"description"`
	PlaceVisits uint64    `bun:"place_visits" json:"placeVisits"`
	Created     time.Time `bun:"created"      json:"created"`
	Updated     time.Time `bun:"updated"      json:"updated"`
}

// ToAPIType converts a UserFavoriteQueryResult to an API Game type.
func (r *UserFavoriteQueryResult) ToAPIType() *apiTypes.Game {
	return &apiTypes.Game{
		ID:          r.GameID,
		Name:        r.Name,
		Description: r.Description,
		PlaceVisits: r.PlaceVisits,
		Created:     r.Created,
		Updated:     r.Updated,
	}
}

// FromAPIGroupRoles creates database types from an API UserGroupRoles type.
func FromAPIGroupRoles(userID uint64, group *apiTypes.UserGroupRoles) (*UserGroup, *GroupInfo) {
	return &UserGroup{
			UserID:   userID,
			GroupID:  group.Group.ID,
			RoleID:   group.Role.ID,
			RoleName: group.Role.Name,
			RoleRank: group.Role.Rank,
		}, &GroupInfo{
			ID:                 group.Group.ID,
			Name:               group.Group.Name,
			Description:        group.Group.Description,
			Owner:              group.Group.Owner,
			Shout:              group.Group.Shout,
			MemberCount:        group.Group.MemberCount,
			HasVerifiedBadge:   group.Group.HasVerifiedBadge,
			IsBuildersClubOnly: group.Group.IsBuildersClubOnly,
			PublicEntryAllowed: group.Group.PublicEntryAllowed,
			IsLocked:           group.Group.IsLocked != nil && *group.Group.IsLocked,
			LastUpdated:        time.Now(),
		}
}

// FromAPIOutfit creates database types from an API Outfit type.
func FromAPIOutfit(userID uint64, outfit *apiTypes.Outfit) (*UserOutfit, *OutfitInfo) {
	return &UserOutfit{
			UserID:   userID,
			OutfitID: outfit.ID,
		}, &OutfitInfo{
			ID:          outfit.ID,
			Name:        outfit.Name,
			IsEditable:  outfit.IsEditable,
			OutfitType:  outfit.OutfitType,
			LastUpdated: time.Now(),
		}
}

// FromAPIFriend creates database types from an API ExtendedFriend type.
func FromAPIFriend(userID uint64, friend *apiTypes.ExtendedFriend) (*UserFriend, *FriendInfo) {
	return &UserFriend{
			UserID:   userID,
			FriendID: friend.ID,
		}, &FriendInfo{
			ID:          friend.ID,
			Name:        friend.Name,
			DisplayName: friend.DisplayName,
			LastUpdated: time.Now(),
		}
}

// FromAPIGame creates database types from an API Game type.
func FromAPIGame(userID uint64, game *apiTypes.Game) (*UserGame, *GameInfo) {
	return &UserGame{
			UserID: userID,
			GameID: game.ID,
		}, &GameInfo{
			ID:          game.ID,
			Name:        game.Name,
			Description: game.Description,
			PlaceVisits: game.PlaceVisits,
			Created:     game.Created,
			Updated:     game.Updated,
			LastUpdated: time.Now(),
		}
}

// FromAPIInventoryAsset creates database types from an API InventoryAsset type.
func FromAPIInventoryAsset(userID uint64, asset *apiTypes.InventoryAsset) (*UserInventory, *InventoryInfo) {
	return &UserInventory{
			UserID:      userID,
			InventoryID: asset.AssetID,
		}, &InventoryInfo{
			ID:          asset.AssetID,
			Name:        asset.Name,
			AssetType:   asset.AssetType,
			Created:     asset.Created,
			LastUpdated: time.Now(),
		}
}

// FromAPIAsset creates database types from an API AssetV2 type.
func FromAPIAsset(userID uint64, asset *apiTypes.AssetV2) (*UserAsset, *AssetInfo) {
	return &UserAsset{
			UserID:           userID,
			AssetID:          asset.ID,
			CurrentVersionID: asset.CurrentVersionID,
		}, &AssetInfo{
			ID:          asset.ID,
			Name:        asset.Name,
			AssetType:   asset.AssetType.ID,
			LastUpdated: time.Now(),
		}
}

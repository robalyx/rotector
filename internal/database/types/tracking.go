package types

import "time"

// GroupMemberTracking monitors confirmed users within groups.
type GroupMemberTracking struct {
	ID           int64     `bun:",pk"`
	LastAppended time.Time `bun:",notnull"`
	LastChecked  time.Time `bun:",notnull"`
	IsFlagged    bool      `bun:",notnull"`
}

// GroupMemberTrackingUser represents a flagged user within a group.
type GroupMemberTrackingUser struct {
	GroupID int64 `bun:",pk"`
	UserID  int64 `bun:",pk"`
}

// OutfitAssetTracking monitors assets that appear in multiple outfits.
type OutfitAssetTracking struct {
	ID           int64     `bun:",pk"`
	LastAppended time.Time `bun:",notnull"`
	LastChecked  time.Time `bun:",notnull"`
	IsFlagged    bool      `bun:",notnull"`
}

// OutfitAssetTrackingOutfit represents an outfit containing a tracked asset.
type OutfitAssetTrackingOutfit struct {
	AssetID   int64 `bun:",pk"`
	TrackedID int64 `bun:",pk"`      // Can be either an outfit ID or user ID
	IsUserID  bool  `bun:",notnull"` // True if TrackedID is actually a user ID
}

// GameTracking monitors games that appear in multiple users' favorites.
type GameTracking struct {
	ID           int64     `bun:",pk"`
	LastAppended time.Time `bun:",notnull"`
	LastChecked  time.Time `bun:",notnull"`
	IsFlagged    bool      `bun:",notnull"`
}

// GameTrackingUser represents a user who has favorited a tracked game.
type GameTrackingUser struct {
	GameID int64 `bun:",pk"`
	UserID int64 `bun:",pk"`
}

// TrackedID represents an ID that can be either an outfit ID or user ID.
type TrackedID struct {
	ID       int64
	IsUserID bool
}

// NewOutfitID creates a TrackedID for an outfit.
func NewOutfitID(id int64) TrackedID {
	return TrackedID{ID: id, IsUserID: false}
}

// NewUserID creates a TrackedID for a user.
func NewUserID(id int64) TrackedID {
	return TrackedID{ID: id, IsUserID: true}
}

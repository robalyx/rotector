package types

import (
	"errors"
	"time"

	"github.com/jaxron/roapi.go/pkg/api/types"
)

// ErrUserNotFound is returned when a user is not found in the database.
var ErrUserNotFound = errors.New("user not found")

// UserType represents the different states a user can be in.
type UserType string

const (
	// UserTypeConfirmed indicates a user has been reviewed and confirmed as inappropriate.
	UserTypeConfirmed UserType = "confirmed"
	// UserTypeFlagged indicates a user needs review for potential violations.
	UserTypeFlagged UserType = "flagged"
	// UserTypeCleared indicates a user was reviewed and found to be appropriate.
	UserTypeCleared UserType = "cleared"
	// UserTypeBanned indicates a user was banned and removed from the system.
	UserTypeBanned UserType = "banned"
	// UserTypeUnflagged indicates a user was not found in the database.
	UserTypeUnflagged UserType = "unflagged"
)

// ExtendedFriend contains additional user information beyond the basic Friend type.
type ExtendedFriend struct {
	types.Friend
	Name             string `json:"name"`             // Username of the friend
	DisplayName      string `json:"displayName"`      // Display name of the friend
	HasVerifiedBadge bool   `json:"hasVerifiedBadge"` // Whether the friend has a verified badge
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
	FollowerCount  uint64                  `bun:",notnull"   json:"followerCount"`
	FollowingCount uint64                  `bun:",notnull"   json:"followingCount"`
	Confidence     float64                 `bun:",notnull"   json:"confidence"`
	LastScanned    time.Time               `bun:",notnull"   json:"lastScanned"`
	LastUpdated    time.Time               `bun:",notnull"   json:"lastUpdated"`
	LastViewed     time.Time               `bun:",notnull"   json:"lastViewed"`
	LastPurgeCheck time.Time               `bun:",notnull"   json:"lastPurgeCheck"`
	ThumbnailURL   string                  `bun:",notnull"   json:"thumbnailUrl"`
	Upvotes        int32                   `bun:",notnull"   json:"upvotes"`
	Downvotes      int32                   `bun:",notnull"   json:"downvotes"`
	Reputation     int32                   `bun:",notnull"   json:"reputation"`
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

// BannedUser extends User to track users that were banned and removed.
// The PurgedAt field shows when the user was removed from the system.
type BannedUser struct {
	User     `json:"user"`
	PurgedAt time.Time `bun:",notnull" json:"purgedAt"`
}

// ReviewUser combines all possible user states into a single structure for review.
type ReviewUser struct {
	User       `json:"user"`
	VerifiedAt time.Time `json:"verifiedAt,omitempty"` // When user was confirmed
	ClearedAt  time.Time `json:"clearedAt,omitempty"`  // When user was cleared
	PurgedAt   time.Time `json:"purgedAt,omitempty"`   // When user was banned/purged
	Status     UserType  `json:"status"`               // Current user status
}

// UserFields represents the fields that can be requested when fetching users.
type UserFields struct {
	// Basic user information
	Basic       bool // ID, Name, DisplayName
	Description bool // Description
	Reason      bool // Reason for flagging
	CreatedAt   bool // Account creation date
	Thumbnail   bool // ThumbnailURL

	// Relationships and content
	Groups  bool // Group memberships
	Outfits bool // User outfits
	Friends bool // Friend list
	Games   bool // Played games

	// Flagged content
	Content bool

	// Statistics
	Followers  bool // FollowerCount, FollowingCount
	Confidence bool // AI confidence score
	Reputation bool // Upvotes, Downvotes, Reputation

	// All timestamps (LastScanned, LastUpdated, LastViewed, LastPurgeCheck)
	Timestamps bool
}

// Columns returns the list of database columns to fetch based on the selected fields.
func (f UserFields) Columns() []string {
	var columns []string

	if f.Basic {
		columns = append(columns, "id", "name", "display_name")
	}
	if f.Description {
		columns = append(columns, "description")
	}
	if f.Reason {
		columns = append(columns, "reason")
	}
	if f.CreatedAt {
		columns = append(columns, "created_at")
	}
	if f.Thumbnail {
		columns = append(columns, "thumbnail_url")
	}
	if f.Groups {
		columns = append(columns, "groups")
	}
	if f.Outfits {
		columns = append(columns, "outfits")
	}
	if f.Friends {
		columns = append(columns, "friends")
	}
	if f.Games {
		columns = append(columns, "games")
	}
	if f.Content {
		columns = append(columns, "flagged_content")
	}
	if f.Followers {
		columns = append(columns, "follower_count", "following_count")
	}
	if f.Confidence {
		columns = append(columns, "confidence")
	}
	if f.Reputation {
		columns = append(columns, "upvotes", "downvotes", "reputation")
	}
	if f.Timestamps {
		columns = append(columns,
			"last_scanned",
			"last_updated",
			"last_viewed",
			"last_purge_check",
		)
	}

	// Select all if no fields specified
	if len(columns) == 0 {
		columns = []string{"*"}
	}

	return columns
}

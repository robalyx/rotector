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
	Reputation bool // Upvotes, Downvotes, Score

	// All timestamps
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
	if f.Timestamps {
		columns = append(columns,
			"last_scanned",
			"last_updated",
			"last_viewed",
			"last_ban_check",
			"last_thumbnail_update",
		)
	}
	if f.Reputation {
		columns = append(columns, "upvotes", "downvotes", "score")
	}

	// Select all if no fields specified
	if len(columns) == 0 {
		columns = []string{"*"}
	}

	return columns
}

package types

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
)

var (
	ErrGroupNotFound    = errors.New("group not found")
	ErrInvalidGroupID   = errors.New("invalid group ID")
	ErrNoGroupsToReview = errors.New("no groups available to review")
)

// Group combines all the information needed to review a group.
type Group struct {
	ID                  uint64            `bun:",pk"        json:"id"`
	UUID                uuid.UUID         `bun:",notnull"   json:"uuid"`
	Name                string            `bun:",notnull"   json:"name"`
	Description         string            `bun:",notnull"   json:"description"`
	Owner               *types.GroupUser  `bun:"type:jsonb" json:"owner"`
	Shout               *types.GroupShout `bun:"type:jsonb" json:"shout"`
	Reason              string            `bun:",notnull"   json:"reason"`
	Confidence          float64           `bun:",notnull"   json:"confidence"`
	LastScanned         time.Time         `bun:",notnull"   json:"lastScanned"`
	LastUpdated         time.Time         `bun:",notnull"   json:"lastUpdated"`
	LastViewed          time.Time         `bun:",notnull"   json:"lastViewed"`
	LastLockCheck       time.Time         `bun:",notnull"   json:"lastLockCheck"`
	IsLocked            bool              `bun:",notnull"   json:"isLocked"`
	ThumbnailURL        string            `bun:",notnull"   json:"thumbnailUrl"`
	LastThumbnailUpdate time.Time         `bun:",notnull"   json:"lastThumbnailUpdate"`
}

// FlaggedGroup extends Group to track groups that need review.
type FlaggedGroup struct {
	Group
}

// ConfirmedGroup extends Group to track groups that have been reviewed and confirmed.
type ConfirmedGroup struct {
	Group
	VerifiedAt time.Time `bun:",notnull" json:"verifiedAt"`
}

// ClearedGroup extends Group to track groups that were cleared during review.
// The ClearedAt field shows when the group was cleared by a moderator.
type ClearedGroup struct {
	Group
	ClearedAt time.Time `bun:",notnull" json:"clearedAt"`
}

// ReviewGroup combines all possible group states into a single structure for review.
type ReviewGroup struct {
	Group      `json:"group"`
	VerifiedAt time.Time      `json:"verifiedAt,omitempty"`
	ClearedAt  time.Time      `json:"clearedAt,omitempty"`
	Status     enum.GroupType `json:"status"`
	Reputation *Reputation    `json:"reputation"`
}

// GroupFields represents the fields that can be requested when fetching groups.
type GroupFields struct {
	// Basic group information
	Basic        bool // ID, Name, Description
	Owner        bool // Owner ID
	Shout        bool // Group shout
	Reason       bool // Reason for flagging
	Thumbnail    bool // ThumbnailURL
	FlaggedUsers bool // FlaggedUsers list

	// Statistics
	Confidence bool // AI confidence score
	Reputation bool // Upvotes, Downvotes, Score

	// All timestamps
	Timestamps bool
}

// Columns returns the list of database columns to fetch based on the selected fields.
func (f GroupFields) Columns() []string {
	var columns []string

	if f.Basic {
		columns = append(columns, "id", "name", "description")
	}
	if f.Owner {
		columns = append(columns, "owner")
	}
	if f.Shout {
		columns = append(columns, "shout")
	}
	if f.Reason {
		columns = append(columns, "reason")
	}
	if f.Thumbnail {
		columns = append(columns, "thumbnail_url")
	}
	if f.FlaggedUsers {
		columns = append(columns, "flagged_users")
	}
	if f.Confidence {
		columns = append(columns, "confidence")
	}
	if f.Reputation {
		columns = append(columns, "upvotes", "downvotes", "score")
	}
	if f.Timestamps {
		columns = append(columns,
			"last_scanned",
			"last_updated",
			"last_viewed",
			"last_lock_check",
			"is_locked",
			"last_thumbnail_update",
		)
	}

	// Select all if no fields specified
	if len(columns) == 0 {
		columns = []string{"*"}
	}

	return columns
}

package types

import (
	"time"

	"github.com/jaxron/roapi.go/pkg/api/types"
)

// Group combines all the information needed to review a group.
type Group struct {
	ID             uint64            `bun:",pk"           json:"id"`
	Name           string            `bun:",notnull"      json:"name"`
	Description    string            `bun:",notnull"      json:"description"`
	Owner          uint64            `bun:",notnull"      json:"owner"`
	Shout          *types.GroupShout `bun:"type:jsonb"    json:"shout"`
	MemberCount    uint64            `bun:",notnull"      json:"memberCount"`
	Reason         string            `bun:",notnull"      json:"reason"`
	Confidence     float64           `bun:",notnull"      json:"confidence"`
	LastScanned    time.Time         `bun:",notnull"      json:"lastScanned"`
	LastUpdated    time.Time         `bun:",notnull"      json:"lastUpdated"`
	LastViewed     time.Time         `bun:",notnull"      json:"lastViewed"`
	LastPurgeCheck time.Time         `bun:",notnull"      json:"lastPurgeCheck"`
	ThumbnailURL   string            `bun:",notnull"      json:"thumbnailUrl"`
	Upvotes        int               `bun:",notnull"      json:"upvotes"`
	Downvotes      int               `bun:",notnull"      json:"downvotes"`
	Reputation     int               `bun:",notnull"      json:"reputation"`
	FlaggedUsers   []uint64          `bun:"type:bigint[]" json:"flaggedUsers"`
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

// LockedGroup extends Group to track groups that were locked and removed.
// The LockedAt field shows when the group was found to be locked.
type LockedGroup struct {
	Group
	LockedAt time.Time `bun:",notnull" json:"lockedAt"`
}

// GroupType represents the different states a group can be in.
type GroupType string

const (
	// GroupTypeConfirmed indicates a group has been reviewed and confirmed as inappropriate.
	GroupTypeConfirmed GroupType = "confirmed"
	// GroupTypeFlagged indicates a group needs review for potential violations.
	GroupTypeFlagged GroupType = "flagged"
	// GroupTypeCleared indicates a group was reviewed and found to be appropriate.
	GroupTypeCleared GroupType = "cleared"
	// GroupTypeLocked indicates a group was locked and removed from the platform.
	GroupTypeLocked GroupType = "locked"
)

// GroupFields represents the fields that can be requested when fetching groups.
type GroupFields struct {
	// Basic group information
	Basic        bool // ID, Name, Description
	Owner        bool // Owner ID
	Shout        bool // Group shout
	MemberCount  bool // Member count
	Reason       bool // Reason for flagging
	Thumbnail    bool // ThumbnailURL
	FlaggedUsers bool // FlaggedUsers list

	// Statistics
	Confidence bool // AI confidence score
	Reputation bool // Upvotes, Downvotes, Reputation

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
	if f.MemberCount {
		columns = append(columns, "member_count")
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

	return columns
}
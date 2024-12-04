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

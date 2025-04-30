package types

import "time"

// GroupMemberTracking monitors confirmed users within groups.
// The LastAppended field helps determine when to purge old tracking data.
type GroupMemberTracking struct {
	ID           uint64    `bun:",pk"`
	LastAppended time.Time `bun:",notnull"`
	LastChecked  time.Time `bun:",notnull"`
	IsFlagged    bool      `bun:",notnull"`
}

// GroupMemberTrackingUser represents a flagged user within a group.
type GroupMemberTrackingUser struct {
	GroupID uint64 `bun:",pk"`
	UserID  uint64 `bun:",pk"`
}

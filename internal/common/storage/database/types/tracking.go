package types

import "time"

// GroupMemberTracking monitors confirmed users within groups.
// The LastAppended field helps determine when to purge old tracking data.
type GroupMemberTracking struct {
	GroupID      uint64    `bun:",pk"`
	FlaggedUsers []uint64  `bun:"type:bigint[]"`
	LastAppended time.Time `bun:",notnull"`
	LastChecked  time.Time `bun:",notnull"`
	IsFlagged    bool      `bun:",notnull"`
}

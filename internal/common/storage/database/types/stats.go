package types

import "time"

// HourlyStats stores cumulative statistics for each hour.
type HourlyStats struct {
	Timestamp       time.Time `bun:",pk"      json:"timestamp"`
	UsersConfirmed  int64     `bun:",notnull" json:"usersConfirmed"`
	UsersFlagged    int64     `bun:",notnull" json:"usersFlagged"`
	UsersCleared    int64     `bun:",notnull" json:"usersCleared"`
	UsersBanned     int64     `bun:",notnull" json:"usersBanned"`
	GroupsConfirmed int64     `bun:",notnull" json:"groupsConfirmed"`
	GroupsFlagged   int64     `bun:",notnull" json:"groupsFlagged"`
	GroupsCleared   int64     `bun:",notnull" json:"groupsCleared"`
	GroupsLocked    int64     `bun:",notnull" json:"groupsLocked"`
}

// UserCounts holds all user-related statistics.
type UserCounts struct {
	Confirmed int
	Flagged   int
	Cleared   int
	Banned    int
}

// GroupCounts holds all group-related statistics.
type GroupCounts struct {
	Confirmed int
	Flagged   int
	Cleared   int
	Locked    int
}

package types

import (
	"time"

	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
)

// ActivityTarget identifies the target of an activity log entry.
// Only one of the fields should be set.
type ActivityTarget struct {
	GuildID   uint64 `bun:",nullzero"`
	DiscordID uint64 `bun:",nullzero"`
	UserID    uint64 `bun:",nullzero"`
	GroupID   uint64 `bun:",nullzero"`
}

// ActivityFilter is used to provide a filter criteria for retrieving activity logs.
type ActivityFilter struct {
	GuildID      uint64
	DiscordID    uint64
	UserID       uint64
	GroupID      uint64
	ReviewerID   uint64
	ActivityType enum.ActivityType
	StartDate    time.Time
	EndDate      time.Time
}

// LogCursor represents a pagination cursor for activity logs.
type LogCursor struct {
	Timestamp time.Time
	Sequence  int64
}

// ActivityLog stores information about moderator actions.
type ActivityLog struct {
	Sequence          int64                  `bun:",pk,autoincrement"`
	ReviewerID        uint64                 `bun:",notnull"`
	ActivityTarget    ActivityTarget         `bun:",embed"`
	ActivityType      enum.ActivityType      `bun:",notnull"`
	ActivityTimestamp time.Time              `bun:",notnull,pk"`
	Details           map[string]interface{} `bun:"type:jsonb"`
}

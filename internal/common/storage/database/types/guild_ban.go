package types

import "time"

// GuildBanLog stores information about guild ban operations.
type GuildBanLog struct {
	ID              int64     `bun:",pk,autoincrement"`
	GuildID         uint64    `bun:",notnull"`
	ReviewerID      uint64    `bun:",notnull"`
	BannedCount     int       `bun:",notnull"`
	FailedCount     int       `bun:",notnull"`
	BannedUserIDs   []uint64  `bun:"banned_user_ids,type:bigint[]"`
	Reason          string    `bun:",type:text"`
	MinGuildsFilter int       `bun:",notnull"`
	Timestamp       time.Time `bun:",notnull"`
}

package types

import "time"

// Reputation tracks voting data for users and groups.
type Reputation struct {
	ID        uint64    `bun:",pk"      json:"id"`
	Upvotes   int32     `bun:",notnull" json:"upvotes"`
	Downvotes int32     `bun:",notnull" json:"downvotes"`
	Score     int32     `bun:",notnull" json:"score"`
	UpdatedAt time.Time `bun:",notnull" json:"updatedAt"`
}

// UserReputation tracks voting data for users.
type UserReputation struct {
	Reputation `bun:"embed"`
}

// GroupReputation tracks voting data for groups.
type GroupReputation struct {
	Reputation `bun:"embed"`
}

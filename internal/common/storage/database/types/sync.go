package types

import (
	"time"
)

// DiscordServerMember represents a member in a Discord server.
type DiscordServerMember struct {
	ServerID  uint64    `bun:",pk"      json:"serverId"`  // Discord server ID
	UserID    uint64    `bun:",pk"      json:"userId"`    // Discord user ID
	JoinedAt  time.Time `bun:",notnull" json:"joinedAt"`  // When the user joined
	UpdatedAt time.Time `bun:",notnull" json:"updatedAt"` // Last update time
}

// DiscordServerInfo represents basic information about a Discord server.
type DiscordServerInfo struct {
	ServerID  uint64    `bun:",pk"      json:"serverId"`  // Discord server ID
	Name      string    `bun:",notnull" json:"name"`      // Server name
	UpdatedAt time.Time `bun:",notnull" json:"updatedAt"` // Last update time
}

// DiscordUserRedaction represents a user's data deletion request.
type DiscordUserRedaction struct {
	UserID     uint64    `bun:",pk"      json:"userId"`     // Discord user ID
	RedactedAt time.Time `bun:",notnull" json:"redactedAt"` // When the data was redacted
	UpdatedAt  time.Time `bun:",notnull" json:"updatedAt"`  // Last update time
}

// UserGuildInfo represents a guild a user is a member of for tracking purposes.
type UserGuildInfo struct {
	ServerID  uint64    `json:"serverId"`  // Discord server ID
	JoinedAt  time.Time `json:"joinedAt"`  // When the user joined
	UpdatedAt time.Time `json:"updatedAt"` // Last update time
}

// GuildCount represents a guild and how many users are members of it.
type GuildCount struct {
	ServerID uint64 `json:"serverId"` // Discord server ID
	Count    int    `json:"count"`    // Number of flagged users in the guild
}

// GuildCursor represents a cursor for paginating through guild memberships.
type GuildCursor struct {
	JoinedAt time.Time `json:"joinedAt"` // Timestamp for cursor position
	ServerID uint64    `json:"serverId"` // Server ID for cursor position
}

// DiscordUserFullScan represents the last time a full guild scan was performed for a Discord user.
type DiscordUserFullScan struct {
	UserID   uint64    `bun:",pk"      json:"userId"`   // Discord user ID
	LastScan time.Time `bun:",notnull" json:"lastScan"` // Last full scan timestamp
}

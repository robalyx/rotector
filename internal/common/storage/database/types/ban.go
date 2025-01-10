package types

import (
	"time"

	"github.com/disgoorg/snowflake/v2"
)

// BanReason represents the reason for a Discord user ban.
type BanReason string

const (
	BanReasonAbuse         BanReason = "abuse"
	BanReasonInappropriate BanReason = "inappropriate"
	BanReasonOther         BanReason = "other"
)

// BanSource indicates what triggered a ban.
type BanSource string

const (
	BanSourceSystem BanSource = "system" // Automatically banned by the system
	BanSourceAdmin  BanSource = "admin"  // Manually banned by an admin
)

// DiscordBan represents a banned Discord user.
type DiscordBan struct {
	ID        snowflake.ID `bun:",pk"`        // Discord user ID
	Reason    BanReason    `bun:",notnull"`   // Reason for the ban
	Source    BanSource    `bun:",notnull"`   // What triggered the ban
	Notes     string       `bun:",type:text"` // Administrative notes about the ban
	BannedBy  uint64       `bun:",notnull"`   // Discord ID of the admin who issued the ban (0 if system)
	BannedAt  time.Time    `bun:",notnull"`   // When the ban was issued
	ExpiresAt *time.Time   `bun:",nullzero"`  // When the ban expires (null for permanent)
	UpdatedAt time.Time    `bun:",notnull"`   // When the record was last updated
}

// IsExpired checks if the ban has expired.
func (b *DiscordBan) IsExpired() bool {
	return b.ExpiresAt != nil && time.Now().After(*b.ExpiresAt)
}

// IsPermanent checks if the ban is permanent.
func (b *DiscordBan) IsPermanent() bool {
	return b.ExpiresAt == nil
}

// IsSystemBan checks if the ban was issued by the system.
func (b *DiscordBan) IsSystemBan() bool {
	return b.Source == BanSourceSystem
}

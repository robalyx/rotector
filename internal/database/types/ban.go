package types

import (
	"time"

	"github.com/robalyx/rotector/internal/database/types/enum"
)

// DiscordBan represents a banned Discord user.
type DiscordBan struct {
	ID        uint64         `bun:",pk"`        // Discord user ID
	Reason    enum.BanReason `bun:",notnull"`   // Reason for the ban
	Source    enum.BanSource `bun:",notnull"`   // What triggered the ban
	Notes     string         `bun:",type:text"` // Administrative notes about the ban
	BannedBy  uint64         `bun:",notnull"`   // Discord ID of the admin who issued the ban (0 if system)
	BannedAt  time.Time      `bun:",notnull"`   // When the ban was issued
	ExpiresAt *time.Time     `bun:",nullzero"`  // When the ban expires (null for permanent)
	UpdatedAt time.Time      `bun:",notnull"`   // When the record was last updated
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
	return b.Source == enum.BanSourceSystem
}

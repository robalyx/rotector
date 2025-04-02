package types

import "time"

// UserConsent tracks user consent to terms of service.
type UserConsent struct {
	DiscordUserID uint64    `bun:",pk"      json:"discordUserId"`
	ConsentedAt   time.Time `bun:",notnull" json:"consentedAt"`
	Version       string    `bun:",notnull" json:"version"`
	AgeVerified   bool      `bun:",notnull" json:"ageVerified"`
}

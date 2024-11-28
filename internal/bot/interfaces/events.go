package interfaces

import (
	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/snowflake/v2"
)

// CommonEvent extracts shared functionality from different Discord event types.
// This allows pagination to work with any interaction event without type checking.
type CommonEvent interface {
	// Client returns the Discord client instance handling this event
	Client() bot.Client

	// ApplicationID returns the bot's application ID for API requests
	ApplicationID() snowflake.ID

	// Token returns the interaction token for responding to the event
	Token() string

	// User returns the Discord user who triggered this event
	User() discord.User

	// GuildID returns the ID of the guild where the event occurred
	// Returns nil for direct message events
	GuildID() *snowflake.ID
}

// These type assertions ensure that all event types properly implement
// the CommonEvent interface at compile time.
var (
	_ CommonEvent = (*events.ApplicationCommandInteractionCreate)(nil)
	_ CommonEvent = (*events.ComponentInteractionCreate)(nil)
	_ CommonEvent = (*events.ModalSubmitInteractionCreate)(nil)
)

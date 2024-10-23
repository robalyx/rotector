package interfaces

import (
	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/snowflake/v2"
)

// CommonEvent is an interface that includes common methods from different event types.
type CommonEvent interface {
	Client() bot.Client
	ApplicationID() snowflake.ID
	Token() string
	User() discord.User
	GuildID() *snowflake.ID
}

// Ensure that all event types implement the CommonEvent interface.
var (
	_ CommonEvent = (*events.ApplicationCommandInteractionCreate)(nil)
	_ CommonEvent = (*events.ComponentInteractionCreate)(nil)
	_ CommonEvent = (*events.ModalSubmitInteractionCreate)(nil)
)

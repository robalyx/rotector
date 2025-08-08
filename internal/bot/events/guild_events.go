package events

import (
	"fmt"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/robalyx/rotector/internal/bot/constants"
	"go.uber.org/zap"
)

// GuildEventHandler manages guild-related events for the bot.
type GuildEventHandler struct {
	logger *zap.Logger
}

// NewGuildEventHandler creates a new instance of the guild event handler.
func NewGuildEventHandler(logger *zap.Logger) *GuildEventHandler {
	return &GuildEventHandler{
		logger: logger.Named("guild_events"),
	}
}

// OnGuildJoin handles the event when the bot joins a new guild.
func (h *GuildEventHandler) OnGuildJoin(event *events.GuildJoin) {
	h.logger.Info("Bot joined a new guild",
		zap.String("guildID", event.Guild.ID.String()),
		zap.String("guild_name", event.Guild.Name))

	// Register commands for this specific guild
	err := h.registerGuildCommands(event)
	if err != nil {
		h.logger.Error("Failed to register guild commands",
			zap.String("guildID", event.Guild.ID.String()),
			zap.Error(err))
	}
}

// registerGuildCommands registers the bot's commands for a specific guild.
func (h *GuildEventHandler) registerGuildCommands(event *events.GuildJoin) error {
	_, err := event.Client().Rest.SetGuildCommands(event.Client().ApplicationID, event.Guild.ID,
		[]discord.ApplicationCommandCreate{
			discord.SlashCommandCreate{
				Name:        constants.RotectorCommandName,
				Description: "Open the moderation interface",
			},
		},
	)
	if err != nil {
		return fmt.Errorf("failed to register guild commands: %w", err)
	}

	h.logger.Debug("Successfully registered guild commands",
		zap.String("guildID", event.Guild.ID.String()))

	return nil
}

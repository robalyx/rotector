package admin

import (
	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/session"
)

// MainBuilder creates the visual layout for the admin menu.
type MainBuilder struct{}

// NewMainBuilder creates a new admin menu builder.
func NewMainBuilder(_ *session.Session) *MainBuilder {
	return &MainBuilder{}
}

// Build creates a Discord message with admin options.
func (b *MainBuilder) Build() *discord.MessageUpdateBuilder {
	// Create admin options
	options := []discord.StringSelectMenuOption{
		discord.NewStringSelectMenuOption("Bot Settings", constants.BotSettingsButtonCustomID).
			WithEmoji(discord.ComponentEmoji{Name: "⚙️"}).
			WithDescription("Configure bot-wide settings"),
		discord.NewStringSelectMenuOption("Delete User", constants.DeleteUserButtonCustomID).
			WithEmoji(discord.ComponentEmoji{Name: "🗑️"}).
			WithDescription("Delete a user from the database"),
		discord.NewStringSelectMenuOption("Delete Group", constants.DeleteGroupButtonCustomID).
			WithEmoji(discord.ComponentEmoji{Name: "🗑️"}).
			WithDescription("Delete a group from the database"),
	}

	// Create embed
	embed := discord.NewEmbedBuilder().
		SetTitle("Admin Menu").
		SetDescription("⚠️ **Warning**: These actions are permanent and cannot be undone.").
		SetColor(constants.DefaultEmbedColor)

	return discord.NewMessageUpdateBuilder().
		SetEmbeds(embed.Build()).
		AddActionRow(
			discord.NewStringSelectMenu(constants.ActionSelectMenuCustomID, "Select Action", options...),
		).
		AddActionRow(
			discord.NewSecondaryButton("◀️", constants.BackButtonCustomID),
		)
}

package admin

import (
	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/session"
)

// Builder creates the visual layout for the admin menu.
type Builder struct{}

// NewBuilder creates a new admin menu builder.
func NewBuilder(_ *session.Session) *Builder {
	return &Builder{}
}

// Build creates a Discord message with admin options.
func (b *Builder) Build() *discord.MessageUpdateBuilder {
	// Create admin options
	options := []discord.StringSelectMenuOption{
		discord.NewStringSelectMenuOption("Bot Settings", constants.BotSettingsButtonCustomID).
			WithEmoji(discord.ComponentEmoji{Name: "⚙️"}).
			WithDescription("Configure bot-wide settings"),
		discord.NewStringSelectMenuOption("Ban Discord User", constants.BanUserButtonCustomID).
			WithEmoji(discord.ComponentEmoji{Name: "🔨"}).
			WithDescription("Ban a Discord user from the system"),
		discord.NewStringSelectMenuOption("Unban Discord User", constants.UnbanUserButtonCustomID).
			WithEmoji(discord.ComponentEmoji{Name: "🔓"}).
			WithDescription("Unban a Discord user from the system"),
		discord.NewStringSelectMenuOption("Delete Roblox User", constants.DeleteUserButtonCustomID).
			WithEmoji(discord.ComponentEmoji{Name: "🗑️"}).
			WithDescription("Delete a Roblox user from the database"),
		discord.NewStringSelectMenuOption("Delete Roblox Group", constants.DeleteGroupButtonCustomID).
			WithEmoji(discord.ComponentEmoji{Name: "🗑️"}).
			WithDescription("Delete a Roblox group from the database"),
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

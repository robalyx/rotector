package setting

import (
	"sort"

	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/session"
)

// BotSettingsBuilder creates the visual layout for bot settings.
type BotSettingsBuilder struct {
	session  *session.Session
	registry *session.SettingRegistry
}

// NewBotSettingsBuilder creates a new bot settings builder.
func NewBotSettingsBuilder(s *session.Session, r *session.SettingRegistry) *BotSettingsBuilder {
	return &BotSettingsBuilder{
		session:  s,
		registry: r,
	}
}

// Build creates a Discord message with the current bot settings.
func (b *BotSettingsBuilder) Build() *discord.MessageUpdateBuilder {
	// Create embed with current settings
	embed := discord.NewEmbedBuilder().
		SetTitle("Bot Settings").
		SetDescription("NOTE: It will take a minute for the settings to propagate.")

	// Get all settings keys and sort them
	keys := make([]string, 0, len(b.registry.BotSettings))
	for key := range b.registry.BotSettings {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	// Add fields for each setting
	for _, key := range keys {
		setting := b.registry.BotSettings[key]
		value := setting.ValueGetter(b.session)
		embed.AddField(setting.Name, value, false)
	}

	embed.SetColor(constants.DefaultEmbedColor)

	// Add interactive components for changing settings
	options := make([]discord.StringSelectMenuOption, 0, len(b.registry.BotSettings))
	for _, key := range keys {
		setting := b.registry.BotSettings[key]
		option := discord.NewStringSelectMenuOption(
			"Change "+setting.Name,
			setting.Key,
		).WithDescription(setting.Description)
		options = append(options, option)
	}

	components := []discord.ContainerComponent{
		discord.NewActionRow(
			discord.NewStringSelectMenu(constants.BotSettingSelectID, "Select a setting to change", options...),
		),
		discord.NewActionRow(
			discord.NewSecondaryButton("Back", constants.BackButtonCustomID),
		),
	}

	return discord.NewMessageUpdateBuilder().
		SetEmbeds(embed.Build()).
		AddContainerComponents(components...)
}

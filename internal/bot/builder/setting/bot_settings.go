package setting

import (
	"sort"

	"github.com/disgoorg/disgo/discord"
	"github.com/rotector/rotector/internal/bot/constants"
	"github.com/rotector/rotector/internal/bot/core/session"
	"github.com/rotector/rotector/internal/common/storage/database/types"
)

// BotSettingsBuilder creates the visual layout for bot settings.
type BotSettingsBuilder struct {
	settings *types.BotSetting
	registry *Registry
}

// NewBotSettingsBuilder creates a new bot settings builder.
func NewBotSettingsBuilder(s *session.Session, r *Registry) *BotSettingsBuilder {
	var settings *types.BotSetting
	s.GetInterface(constants.SessionKeyBotSettings, &settings)

	return &BotSettingsBuilder{
		settings: settings,
		registry: r,
	}
}

// Build creates a Discord message with the current bot settings.
func (b *BotSettingsBuilder) Build() *discord.MessageUpdateBuilder {
	// Create embed with current settings
	embed := discord.NewEmbedBuilder().
		SetTitle("Bot Settings")

	// Get all settings keys and sort them
	keys := make([]string, 0, len(b.registry.BotSettings))
	for key := range b.registry.BotSettings {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	// Add fields for each setting
	for _, key := range keys {
		setting := b.registry.BotSettings[key]
		value := setting.ValueGetter(nil, b.settings)
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

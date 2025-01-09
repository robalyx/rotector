package setting

import (
	"sort"

	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
)

// UserSettingsBuilder creates the visual layout for user settings.
type UserSettingsBuilder struct {
	settings    *types.UserSetting
	botSettings *types.BotSetting
	registry    *Registry
}

// NewUserSettingsBuilder creates a new user settings builder.
func NewUserSettingsBuilder(s *session.Session, r *Registry) *UserSettingsBuilder {
	var settings *types.UserSetting
	s.GetInterface(constants.SessionKeyUserSettings, &settings)
	var botSettings *types.BotSetting
	s.GetInterface(constants.SessionKeyBotSettings, &botSettings)

	return &UserSettingsBuilder{
		settings:    settings,
		botSettings: botSettings,
		registry:    r,
	}
}

// Build creates a Discord message with the current settings displayed in an embed
// and adds select menus for changing each setting.
func (b *UserSettingsBuilder) Build() *discord.MessageUpdateBuilder {
	// Get all settings keys and sort them
	keys := make([]string, 0, len(b.registry.UserSettings))
	for key := range b.registry.UserSettings {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	// Create base options
	options := make([]discord.StringSelectMenuOption, 0, len(b.registry.UserSettings))

	// Add options for each user setting in alphabetical order
	for _, key := range keys {
		setting := b.registry.UserSettings[key]
		option := discord.NewStringSelectMenuOption(
			"Change "+setting.Name,
			setting.Key,
		).WithDescription(setting.Description)
		options = append(options, option)
	}

	// Create embed with current settings values
	embed := discord.NewEmbedBuilder().
		SetTitle("User Settings")

	// Add fields for each setting
	for _, key := range keys {
		setting := b.registry.UserSettings[key]
		value := setting.ValueGetter(b.settings, b.botSettings)
		embed.AddField(setting.Name, value, true)
	}

	embed.SetColor(constants.DefaultEmbedColor)

	// Add interactive components for changing settings
	components := []discord.ContainerComponent{
		discord.NewActionRow(
			discord.NewStringSelectMenu(constants.UserSettingSelectID, "Select a setting to change", options...),
		),
		discord.NewActionRow(
			discord.NewSecondaryButton("Back", constants.BackButtonCustomID),
		),
	}

	return discord.NewMessageUpdateBuilder().
		SetEmbeds(embed.Build()).
		AddContainerComponents(components...)
}

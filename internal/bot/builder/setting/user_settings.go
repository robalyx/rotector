package setting

import (
	"sort"

	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/session"
)

// UserSettingsBuilder creates the visual layout for user settings.
type UserSettingsBuilder struct {
	session  *session.Session
	registry *session.SettingRegistry
}

// NewUserSettingsBuilder creates a new user settings builder.
func NewUserSettingsBuilder(s *session.Session, r *session.SettingRegistry) *UserSettingsBuilder {
	return &UserSettingsBuilder{
		session:  s,
		registry: r,
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
		value := setting.ValueGetter(b.session)
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

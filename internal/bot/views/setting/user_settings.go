package setting

import (
	"sort"
	"strings"

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

// Build creates a Discord message with the current settings.
func (b *UserSettingsBuilder) Build() *discord.MessageUpdateBuilder {
	// Get all settings keys and sort them
	keys := make([]string, 0, len(b.registry.UserSettings))
	for key := range b.registry.UserSettings {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	// Create main info container
	var content strings.Builder
	content.WriteString("## User Settings\n\n")

	// Add fields for each setting
	for _, key := range keys {
		setting := b.registry.UserSettings[key]
		value := setting.ValueGetter(b.session)
		content.WriteString("### " + setting.Name + "\n")
		content.WriteString(value + "\n")
	}

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

	// Create container with all components
	container := discord.NewContainer(
		discord.NewTextDisplay(content.String()),
		discord.NewLargeSeparator(),
		discord.NewActionRow(
			discord.NewStringSelectMenu(constants.UserSettingSelectID, "Select a setting to change", options...),
		),
	).WithAccentColor(constants.DefaultContainerColor)

	return discord.NewMessageUpdateBuilder().
		AddComponents(
			container,
			discord.NewActionRow(
				discord.NewSecondaryButton("Back", constants.BackButtonCustomID),
			),
		)
}

package builders

import (
	"fmt"
	"strconv"

	"github.com/disgoorg/disgo/discord"
	"github.com/rotector/rotector/internal/bot/constants"
	"github.com/rotector/rotector/internal/bot/session"
	"github.com/rotector/rotector/internal/bot/utils"
	"github.com/rotector/rotector/internal/common/database"
)

// UserSettingsEmbed builds the embed and components for the user settings menu.
type UserSettingsEmbed struct {
	settings *database.UserSetting
}

// NewUserSettingsEmbed creates a new UserSettingsEmbed.
func NewUserSettingsEmbed(s *session.Session) *UserSettingsEmbed {
	var settings *database.UserSetting
	s.GetInterface(constants.SessionKeyUserSettings, &settings)

	return &UserSettingsEmbed{
		settings: settings,
	}
}

// Build constructs and returns the discord.MessageUpdateBuilder for user settings.
func (b *UserSettingsEmbed) Build() *discord.MessageUpdateBuilder {
	embed := discord.NewEmbedBuilder().
		SetTitle("User Settings").
		AddField("Streamer Mode", strconv.FormatBool(b.settings.StreamerMode), true).
		AddField("Default Sort", b.settings.DefaultSort, true).
		SetColor(constants.DefaultEmbedColor)

	components := []discord.ContainerComponent{
		discord.NewActionRow(
			discord.NewStringSelectMenu(constants.UserSettingSelectID, "Select a setting to change",
				discord.NewStringSelectMenuOption("Change Streamer Mode", constants.StreamerModeOption),
				discord.NewStringSelectMenuOption("Change Default Sort", constants.DefaultSortOption),
			),
		),
		discord.NewActionRow(
			discord.NewSecondaryButton("Back", constants.BackButtonCustomID),
		),
	}

	return discord.NewMessageUpdateBuilder().
		SetEmbeds(embed.Build()).
		AddContainerComponents(components...)
}

// GuildSettingsEmbed builds the embed and components for the guild settings menu.
type GuildSettingsEmbed struct {
	settings *database.GuildSetting
	roles    []discord.Role
}

// NewGuildSettingsEmbed creates a new GuildSettingsEmbed.
func NewGuildSettingsEmbed(s *session.Session) *GuildSettingsEmbed {
	var settings *database.GuildSetting
	s.GetInterface(constants.SessionKeyGuildSettings, &settings)
	var roles []discord.Role
	s.GetInterface(constants.SessionKeyRoles, &roles)

	return &GuildSettingsEmbed{
		settings: settings,
		roles:    roles,
	}
}

// Build constructs and returns the discord.MessageUpdateBuilder for guild settings.
func (b *GuildSettingsEmbed) Build() *discord.MessageUpdateBuilder {
	embed := discord.NewEmbedBuilder().
		SetTitle("Guild Settings").
		AddField("Whitelisted Roles", utils.FormatWhitelistedRoles(b.settings.WhitelistedRoles, b.roles), false).
		SetColor(constants.DefaultEmbedColor)

	components := []discord.ContainerComponent{
		discord.NewActionRow(
			discord.NewStringSelectMenu(constants.GuildSettingSelectID, "Select roles to whitelist",
				discord.NewStringSelectMenuOption("Change Whitelisted Roles", constants.WhitelistedRolesOption),
			),
		),
		discord.NewActionRow(
			discord.NewSecondaryButton("Back", constants.BackButtonCustomID),
		),
	}

	return discord.NewMessageUpdateBuilder().
		SetEmbeds(embed.Build()).
		AddContainerComponents(components...)
}

// SettingChangeBuilder builds the embed and components for changing a specific setting.
type SettingChangeBuilder struct {
	settingName  string
	settingType  string
	currentValue string
	customID     string
	options      []discord.StringSelectMenuOption
}

// NewSettingChangeBuilder creates a new SettingChangeBuilder.
func NewSettingChangeBuilder(s *session.Session) *SettingChangeBuilder {
	return &SettingChangeBuilder{
		settingName:  s.GetString(constants.SessionKeySettingName),
		settingType:  s.GetString(constants.SessionKeySettingType),
		currentValue: s.GetString(constants.SessionKeyCurrentValue),
		customID:     s.GetString(constants.SessionKeyCustomID),
	}
}

// AddOption adds an option to the setting change menu.
func (b *SettingChangeBuilder) AddOptions(options ...discord.StringSelectMenuOption) *SettingChangeBuilder {
	b.options = append(b.options, options...)
	return b
}

// Build constructs and returns the discord.MessageUpdateBuilder for changing a setting.
func (b *SettingChangeBuilder) Build() *discord.MessageUpdateBuilder {
	embed := discord.NewEmbedBuilder().
		SetTitle("Change " + b.settingName).
		SetDescription("Current value: " + b.currentValue).
		SetColor(constants.DefaultEmbedColor)

	components := []discord.ContainerComponent{
		discord.NewActionRow(
			discord.NewStringSelectMenu(b.customID, "Select new value", b.options...),
		),
		discord.NewActionRow(
			discord.NewSecondaryButton("Back to Settings", fmt.Sprintf("%s_%s", b.settingType, constants.BackButtonCustomID)),
		),
	}

	return discord.NewMessageUpdateBuilder().
		SetEmbeds(embed.Build()).
		AddContainerComponents(components...)
}

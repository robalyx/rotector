package builders

import (
	"fmt"
	"strconv"

	"github.com/disgoorg/disgo/discord"
	"github.com/rotector/rotector/internal/bot/constants"
	"github.com/rotector/rotector/internal/common/database"
)

// UserSettingsEmbed builds the embed and components for the user settings menu.
type UserSettingsEmbed struct {
	preferences *database.UserPreference
}

// NewUserSettingsEmbed creates a new UserSettingsEmbed.
func NewUserSettingsEmbed(preferences *database.UserPreference) *UserSettingsEmbed {
	return &UserSettingsEmbed{
		preferences: preferences,
	}
}

// Build constructs and returns the discord.MessageUpdateBuilder for user settings.
func (b *UserSettingsEmbed) Build() *discord.MessageUpdateBuilder {
	embed := discord.NewEmbedBuilder().
		SetTitle("User Preferences").
		AddField("Streamer Mode", strconv.FormatBool(b.preferences.StreamerMode), true).
		AddField("Default Sort", b.preferences.DefaultSort, true).
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
	currentValue string
	roles        []discord.Role
}

// NewGuildSettingsEmbed creates a new GuildSettingsEmbed.
func NewGuildSettingsEmbed(currentValue string, roles []discord.Role) *GuildSettingsEmbed {
	return &GuildSettingsEmbed{
		currentValue: currentValue,
		roles:        roles,
	}
}

// Build constructs and returns the discord.MessageUpdateBuilder for guild settings.
func (b *GuildSettingsEmbed) Build() *discord.MessageUpdateBuilder {
	embed := discord.NewEmbedBuilder().
		SetTitle("Guild Settings").
		AddField("Whitelisted Roles", b.currentValue, false).
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
func NewSettingChangeBuilder(settingName, settingType, currentValue, customID string) *SettingChangeBuilder {
	return &SettingChangeBuilder{
		settingName:  settingName,
		settingType:  settingType,
		currentValue: currentValue,
		customID:     customID,
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

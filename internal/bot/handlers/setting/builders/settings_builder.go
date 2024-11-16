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

// UserSettingsEmbed creates the visual layout for user preferences.
type UserSettingsEmbed struct {
	settings    *database.UserSetting
	botSettings *database.BotSetting
	userID      uint64
}

// NewUserSettingsEmbed loads user settings from the session state to create
// a new embed builder.
func NewUserSettingsEmbed(s *session.Session) *UserSettingsEmbed {
	var settings *database.UserSetting
	s.GetInterface(constants.SessionKeyUserSettings, &settings)
	var botSettings *database.BotSetting
	s.GetInterface(constants.SessionKeyBotSettings, &botSettings)

	return &UserSettingsEmbed{
		settings:    settings,
		botSettings: botSettings,
		userID:      s.GetUint64(constants.SessionKeyUserID),
	}
}

// Build creates a Discord message with the current settings displayed in an embed
// and adds select menus for changing each setting.
func (b *UserSettingsEmbed) Build() *discord.MessageUpdateBuilder {
	// Create base options
	options := []discord.StringSelectMenuOption{
		discord.NewStringSelectMenuOption("Change Streamer Mode", constants.StreamerModeOption).
			WithDescription("Toggle censoring of sensitive information"),
		discord.NewStringSelectMenuOption("Change Default Sort", constants.DefaultSortOption).
			WithDescription("Set what users are shown first in the review menu"),
	}

	// Add review mode option only for reviewers
	if b.botSettings.IsReviewer(b.userID) {
		options = append(options,
			discord.NewStringSelectMenuOption("Change Review Mode", constants.ReviewModeOption).
				WithDescription("Switch between training and standard review modes"),
		)
	}

	// Create embed with current settings values
	embed := discord.NewEmbedBuilder().
		SetTitle("User Settings").
		AddField("Streamer Mode", strconv.FormatBool(b.settings.StreamerMode), true).
		AddField("Default Sort", b.settings.DefaultSort, true).
		AddField("Review Mode", database.FormatReviewMode(b.settings.ReviewMode), true).
		SetColor(constants.DefaultEmbedColor)

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

// BotSettingsEmbed creates the visual layout for bot settings.
type BotSettingsEmbed struct {
	settings *database.BotSetting
}

// NewBotSettingsEmbed loads bot settings from the session state.
func NewBotSettingsEmbed(s *session.Session) *BotSettingsEmbed {
	var settings *database.BotSetting
	s.GetInterface(constants.SessionKeyBotSettings, &settings)

	return &BotSettingsEmbed{
		settings: settings,
	}
}

// Build creates a Discord message with the current bot settings.
func (b *BotSettingsEmbed) Build() *discord.MessageUpdateBuilder {
	// Create embed with current settings
	embed := discord.NewEmbedBuilder().
		SetTitle("Bot Settings").
		AddField("Reviewer IDs", utils.FormatIDs(b.settings.ReviewerIDs), false).
		AddField("Admin IDs", utils.FormatIDs(b.settings.AdminIDs), false).
		SetColor(constants.DefaultEmbedColor)

	// Add interactive components for changing settings
	components := []discord.ContainerComponent{
		discord.NewActionRow(
			discord.NewStringSelectMenu(constants.BotSettingSelectID, "Select a setting to change",
				discord.NewStringSelectMenuOption("Change Reviewer IDs", constants.ReviewerIDsOption).
					WithDescription("Set which users can review using the bot"),
				discord.NewStringSelectMenuOption("Change Admin IDs", constants.AdminIDsOption).
					WithDescription("Set which users can access bot settings"),
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

// SettingChangeBuilder creates a generic settings change menu.
type SettingChangeBuilder struct {
	settingName  string
	settingType  string
	currentValue string
	customID     string
	options      []discord.StringSelectMenuOption
}

// NewSettingChangeBuilder loads setting information from the session state
// to create a new change menu builder.
func NewSettingChangeBuilder(s *session.Session) *SettingChangeBuilder {
	return &SettingChangeBuilder{
		settingName:  s.GetString(constants.SessionKeySettingName),
		settingType:  s.GetString(constants.SessionKeySettingType),
		currentValue: s.GetString(constants.SessionKeyCurrentValue),
		customID:     s.GetString(constants.SessionKeyCustomID),
	}
}

// AddOptions adds selectable choices to the settings change menu.
func (b *SettingChangeBuilder) AddOptions(options ...discord.StringSelectMenuOption) *SettingChangeBuilder {
	b.options = append(b.options, options...)
	return b
}

// Build creates a Discord message showing the current setting value and
// providing a select menu with available options.
func (b *SettingChangeBuilder) Build() *discord.MessageUpdateBuilder {
	// Create embed showing current value
	embed := discord.NewEmbedBuilder().
		SetTitle("Change " + b.settingName).
		SetDescription("Current value: " + b.currentValue).
		SetColor(constants.DefaultEmbedColor)

	// Add select menu with options and back button
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

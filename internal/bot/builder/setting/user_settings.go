package setting

import (
	"strconv"

	"github.com/disgoorg/disgo/discord"
	"github.com/rotector/rotector/internal/bot/constants"
	"github.com/rotector/rotector/internal/bot/core/session"
	"github.com/rotector/rotector/internal/common/storage/database/models"
)

// UserSettingsBuilder creates the visual layout for user preferences.
type UserSettingsBuilder struct {
	settings    *models.UserSetting
	botSettings *models.BotSetting
	userID      uint64
}

// NewUserSettingsBuilder creates a new user settings builder.
func NewUserSettingsBuilder(s *session.Session) *UserSettingsBuilder {
	var settings *models.UserSetting
	s.GetInterface(constants.SessionKeyUserSettings, &settings)
	var botSettings *models.BotSetting
	s.GetInterface(constants.SessionKeyBotSettings, &botSettings)

	return &UserSettingsBuilder{
		settings:    settings,
		botSettings: botSettings,
		userID:      s.GetUint64(constants.SessionKeyUserID),
	}
}

// Build creates a Discord message with the current settings displayed in an embed
// and adds select menus for changing each setting.
func (b *UserSettingsBuilder) Build() *discord.MessageUpdateBuilder {
	// Create base options
	options := []discord.StringSelectMenuOption{
		discord.NewStringSelectMenuOption("Change Streamer Mode", constants.StreamerModeOption).
			WithDescription("Toggle censoring of sensitive information"),
		discord.NewStringSelectMenuOption("Change User Default Sort", constants.UserDefaultSortOption).
			WithDescription("Set what users are shown first in the review menu"),
		discord.NewStringSelectMenuOption("Change Group Default Sort", constants.GroupDefaultSortOption).
			WithDescription("Set what groups are shown first in the review menu"),
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
		AddField("User Default Sort", b.settings.UserDefaultSort, true).
		AddField("Group Default Sort", b.settings.GroupDefaultSort, true).
		AddField("Review Mode", models.FormatReviewMode(b.settings.ReviewMode), true).
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

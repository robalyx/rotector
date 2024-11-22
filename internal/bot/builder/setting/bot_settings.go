package setting

import (
	"github.com/disgoorg/disgo/discord"
	"github.com/rotector/rotector/internal/bot/constants"
	"github.com/rotector/rotector/internal/bot/core/session"
	"github.com/rotector/rotector/internal/bot/utils"
	"github.com/rotector/rotector/internal/common/storage/database/models"
)

// BotSettingsBuilder creates the visual layout for bot settings.
type BotSettingsBuilder struct {
	settings *models.BotSetting
}

// NewBotSettingsBuilder creates a new bot settings builder.
func NewBotSettingsBuilder(s *session.Session) *BotSettingsBuilder {
	var settings *models.BotSetting
	s.GetInterface(constants.SessionKeyBotSettings, &settings)

	return &BotSettingsBuilder{
		settings: settings,
	}
}

// Build creates a Discord message with the current bot settings.
func (b *BotSettingsBuilder) Build() *discord.MessageUpdateBuilder {
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

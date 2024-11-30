package setting

import (
	"fmt"

	"github.com/disgoorg/disgo/discord"
	"github.com/rotector/rotector/internal/bot/constants"
	"github.com/rotector/rotector/internal/bot/core/session"
	"github.com/rotector/rotector/internal/common/storage/database/models"
)

// UpdateBuilder creates a generic settings change menu.
type UpdateBuilder struct {
	settingName  string
	settingType  string
	currentValue string
	customID     string
	setting      models.Setting
}

// NewUpdateBuilder creates a new update builder.
func NewUpdateBuilder(s *session.Session) *UpdateBuilder {
	var setting models.Setting
	s.GetInterface(constants.SessionKeySetting, &setting)

	return &UpdateBuilder{
		settingName:  s.GetString(constants.SessionKeySettingName),
		settingType:  s.GetString(constants.SessionKeySettingType),
		currentValue: s.GetString(constants.SessionKeyCurrentValue),
		customID:     s.GetString(constants.SessionKeyCustomID),
		setting:      setting,
	}
}

// Build creates a Discord message showing the current setting value and
// providing appropriate input controls based on the setting type.
func (b *UpdateBuilder) Build() *discord.MessageUpdateBuilder {
	embed := discord.NewEmbedBuilder().
		SetTitle("Change " + b.setting.Name).
		SetDescription(fmt.Sprintf("%s\n\nCurrent value:\n%s", b.setting.Description, b.currentValue)).
		SetColor(constants.DefaultEmbedColor)

	components := b.buildComponents()

	return discord.NewMessageUpdateBuilder().
		SetEmbeds(embed.Build()).
		AddContainerComponents(components...)
}

// buildComponents creates the interactive components based on setting type.
func (b *UpdateBuilder) buildComponents() []discord.ContainerComponent {
	var components []discord.ContainerComponent

	// Add setting-specific components
	switch b.setting.Type {
	case models.SettingTypeBool:
		components = append(components, b.buildBooleanComponents())
	case models.SettingTypeEnum:
		components = append(components, b.buildEnumComponents())
	case models.SettingTypeID, models.SettingTypeNumber, models.SettingTypeText:
		components = append(components, b.buildModalComponents())
	}

	// Add back button
	components = append(components, discord.NewActionRow(
		discord.NewSecondaryButton("Back", fmt.Sprintf("%s_%s", b.settingType, constants.BackButtonCustomID)),
	))

	return components
}

func (b *UpdateBuilder) buildBooleanComponents() discord.ContainerComponent {
	return discord.NewActionRow(
		discord.NewStringSelectMenu(b.customID, "Select new value",
			discord.NewStringSelectMenuOption("Enable", "true"),
			discord.NewStringSelectMenuOption("Disable", "false"),
		),
	)
}

func (b *UpdateBuilder) buildEnumComponents() discord.ContainerComponent {
	options := make([]discord.StringSelectMenuOption, 0, len(b.setting.Options))
	for _, opt := range b.setting.Options {
		option := discord.NewStringSelectMenuOption(opt.Label, opt.Value).
			WithDescription(opt.Description)
		if opt.Emoji != "" {
			option = option.WithEmoji(discord.ComponentEmoji{Name: opt.Emoji})
		}
		options = append(options, option)
	}

	return discord.NewActionRow(
		discord.NewStringSelectMenu(b.customID, "Select new value", options...),
	)
}

func (b *UpdateBuilder) buildModalComponents() discord.ContainerComponent {
	var buttonText string
	switch b.setting.Type {
	case models.SettingTypeID:
		buttonText = "Add/Remove ID"
	case models.SettingTypeNumber:
		buttonText = "Set Value"
	case models.SettingTypeText:
		buttonText = "Set Message"
	} //exhaustive:ignore

	return discord.NewActionRow(
		discord.NewPrimaryButton(buttonText, b.customID+constants.ModalOpenSuffix),
	)
}

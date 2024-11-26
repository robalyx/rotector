package setting

import (
	"fmt"
	"strings"

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
	description := b.buildDescription()
	embed := discord.NewEmbedBuilder().
		SetTitle("Change " + b.setting.Name).
		SetDescription(description).
		SetColor(constants.DefaultEmbedColor)

	components := b.buildComponents()

	return discord.NewMessageUpdateBuilder().
		SetEmbeds(embed.Build()).
		AddContainerComponents(components...)
}

// buildDescription creates the description text for the setting embed.
func (b *UpdateBuilder) buildDescription() string {
	description := b.setting.Description + "\n\n"

	if b.setting.Type == models.SettingTypeID {
		return b.buildIDDescription(description)
	}
	return description + "Current value: " + b.currentValue
}

// buildIDDescription formats the description for ID type settings.
func (b *UpdateBuilder) buildIDDescription(baseDescription string) string {
	description := baseDescription + "Current IDs:\n"
	if b.currentValue == "[]" || b.currentValue == "" {
		return description + "None"
	}

	// Remove brackets and split the string
	idStr := strings.Trim(b.currentValue, "[]")
	if idStr == "" {
		return description + "None"
	}

	ids := strings.Split(idStr, " ")
	for _, id := range ids {
		description += fmt.Sprintf("<@%s>\n", id)
	}
	return description
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
	case models.SettingTypeID:
		components = append(components, b.buildIDComponents())
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

func (b *UpdateBuilder) buildIDComponents() discord.ContainerComponent {
	return discord.NewActionRow(
		discord.NewPrimaryButton("Add/Remove ID", b.customID+constants.ModalOpenSuffix),
	)
}

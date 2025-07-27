package setting

import (
	"fmt"
	"strings"

	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/database/types/enum"
)

// UpdateBuilder creates a generic settings change menu.
type UpdateBuilder struct {
	session      *session.Session
	setting      *session.Setting
	settingName  string
	settingType  string
	currentValue string
	customID     string
	page         int
	offset       int
	totalItems   int
	totalPages   int
}

// NewUpdateBuilder creates a new update builder.
func NewUpdateBuilder(s *session.Session) *UpdateBuilder {
	return &UpdateBuilder{
		session:      s,
		setting:      session.SettingValue.Get(s),
		settingName:  session.SettingName.Get(s),
		settingType:  session.SettingType.Get(s),
		currentValue: session.SettingDisplay.Get(s),
		customID:     session.SettingCustomID.Get(s),
		page:         session.PaginationPage.Get(s),
		offset:       session.PaginationOffset.Get(s),
		totalItems:   session.PaginationTotalItems.Get(s),
		totalPages:   session.PaginationTotalPages.Get(s),
	}
}

// Build creates a Discord message showing the current setting value and
// providing appropriate input controls based on the setting type.
func (b *UpdateBuilder) Build() *discord.MessageUpdateBuilder {
	// Create main info container
	var content strings.Builder
	content.WriteString(fmt.Sprintf("## Change %s\n", b.setting.Name))
	content.WriteString(b.setting.Description + "\n")

	// Add fields based on setting type
	switch b.setting.Type {
	case enum.SettingTypeID:
		content.WriteString(b.buildIDContent())
	case enum.SettingTypeBool, enum.SettingTypeEnum, enum.SettingTypeNumber, enum.SettingTypeText:
		content.WriteString("### Current Value\n")
		content.WriteString(b.currentValue + "\n")
	}

	// Build interactive components
	var components []discord.ContainerSubComponent

	components = append(components, discord.NewTextDisplay(content.String()))
	components = append(components, discord.NewLargeSeparator())

	// Add type-specific components
	switch b.setting.Type {
	case enum.SettingTypeBool:
		components = append(components, b.buildBooleanComponents())
	case enum.SettingTypeEnum:
		components = append(components, b.buildEnumComponents())
	case enum.SettingTypeID, enum.SettingTypeNumber, enum.SettingTypeText:
		modalComponents := b.buildModalComponents()
		components = append(components, modalComponents...)
	}

	// Create container with all components
	container := discord.NewContainer(components...).
		WithAccentColor(constants.DefaultContainerColor)

	return discord.NewMessageUpdateBuilder().
		AddComponents(
			container,
			discord.NewActionRow(
				discord.NewSecondaryButton("Back", fmt.Sprintf("%s_%s", b.settingType, constants.BackButtonCustomID)),
			),
		)
}

// buildIDContent creates the content for ID type settings.
func (b *UpdateBuilder) buildIDContent() string {
	var content strings.Builder

	// Get the appropriate ID list based on setting key
	var ids []uint64

	switch b.setting.Key {
	case constants.ReviewerIDsOption:
		ids = session.BotReviewerIDs.Get(b.session)
	case constants.AdminIDsOption:
		ids = session.BotAdminIDs.Get(b.session)
	default:
		return "### Error\nUnknown ID setting type\n"
	}

	if len(ids) == 0 {
		return "### No IDs Set\nUse the button below to add IDs\n"
	}

	// Use stored pagination state
	start := b.offset
	end := min(start+constants.SettingsIDsPerPage, len(ids))

	// Add fields for this page
	for _, id := range ids[start:end] {
		content.WriteString(fmt.Sprintf("### ID: %d\n<@%d>\n", id, id))
	}

	return content.String()
}

// buildBooleanComponents creates the components for boolean settings.
func (b *UpdateBuilder) buildBooleanComponents() discord.ContainerSubComponent {
	return discord.NewActionRow(
		discord.NewStringSelectMenu(b.customID, "Select new value",
			discord.NewStringSelectMenuOption("Enable", "true"),
			discord.NewStringSelectMenuOption("Disable", "false"),
		),
	)
}

// buildEnumComponents creates the components for enum settings.
func (b *UpdateBuilder) buildEnumComponents() discord.ContainerSubComponent {
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

// buildModalComponents creates the components for modal settings.
func (b *UpdateBuilder) buildModalComponents() []discord.ContainerSubComponent {
	var components []discord.ContainerSubComponent

	// Add modal button
	var buttonText string

	switch b.setting.Type {
	case enum.SettingTypeID:
		buttonText = "Add/Remove ID"
	case enum.SettingTypeNumber:
		buttonText = "Set Value"
	case enum.SettingTypeText:
		buttonText = "Set Description"
	default:
		buttonText = "Set Value"
	}

	components = append(components, discord.NewActionRow(
		discord.NewPrimaryButton(buttonText, b.customID+constants.ModalOpenSuffix),
	))

	// Add pagination buttons for ID type settings
	if b.setting.Type == enum.SettingTypeID {
		components = append(components, b.buildPaginationButtons())
	}

	return components
}

// buildPaginationButtons creates the standard pagination buttons.
func (b *UpdateBuilder) buildPaginationButtons() discord.ContainerSubComponent {
	return discord.NewActionRow(
		discord.NewSecondaryButton("⏮️", string(session.ViewerFirstPage)).WithDisabled(b.page == 0),
		discord.NewSecondaryButton("◀️", string(session.ViewerPrevPage)).WithDisabled(b.page == 0),
		discord.NewSecondaryButton("▶️", string(session.ViewerNextPage)).WithDisabled(b.page >= b.totalPages-1),
		discord.NewSecondaryButton("⏭️", string(session.ViewerLastPage)).WithDisabled(b.page >= b.totalPages-1),
	)
}

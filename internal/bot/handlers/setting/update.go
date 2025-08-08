package setting

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/interaction"
	"github.com/robalyx/rotector/internal/bot/core/session"
	view "github.com/robalyx/rotector/internal/bot/views/setting"
	"github.com/robalyx/rotector/internal/database/types/enum"
	"go.uber.org/zap"
)

// UpdateMenu handles the interface for changing individual settings.
type UpdateMenu struct {
	layout *Layout
	page   *interaction.Page
}

// NewUpdateMenu creates a Menu and sets up its page with message builders and
// interaction handlers for changing settings.
func NewUpdateMenu(l *Layout) *UpdateMenu {
	m := &UpdateMenu{layout: l}
	m.page = &interaction.Page{
		Name: constants.SettingUpdatePageName,
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return view.NewUpdateBuilder(s).Build()
		},
		ShowHandlerFunc:   m.Show,
		SelectHandlerFunc: m.handleSettingChange,
		ButtonHandlerFunc: m.handleSettingButton,
		ModalHandlerFunc:  m.handleSettingModal,
	}

	return m
}

// Show prepares and displays the settings change interface.
func (m *UpdateMenu) Show(_ *interaction.Context, s *session.Session) {
	// Get the setting definition
	settingType := session.SettingType.Get(s)
	settingKey := session.SettingCustomID.Get(s)
	setting := m.getSetting(settingType, settingKey)

	// Store setting information in session
	session.SettingName.Set(s, setting.Name)
	session.SettingValue.Set(s, setting)
	_ = m.calculatePagination(s)

	// Get current value based on setting type
	currentValue := setting.ValueGetter(s)
	session.SettingDisplay.Set(s, currentValue)
}

// handleSettingChange processes setting value changes.
func (m *UpdateMenu) handleSettingChange(ctx *interaction.Context, s *session.Session, customID, option string) {
	settingType := session.SettingType.Get(s)
	settingKey := session.SettingCustomID.Get(s)
	setting := m.getSetting(settingType, settingKey)

	// Validate the new value
	if err := m.validateSettingValue(s, setting, option); err != nil {
		ctx.Cancel(fmt.Sprintf("Failed to validate setting value: %v", err))
		return
	}

	// Update the setting
	if err := setting.ValueUpdater(customID, []string{option}, s); err != nil {
		ctx.Error(fmt.Sprintf("Failed to update setting: %v", err))
		return
	}

	ctx.Reload("")
}

// handleSettingButton processes button interactions.
func (m *UpdateMenu) handleSettingButton(ctx *interaction.Context, s *session.Session, customID string) {
	// Handle back button
	split := strings.Split(customID, "_")
	if len(split) > 1 && split[1] == constants.BackButtonCustomID {
		ctx.NavigateBack("")
		return
	}

	// Get the current setting
	setting := session.SettingValue.Get(s)

	// Handle pagination buttons
	action := session.ViewerAction(customID)
	switch action {
	case session.ViewerFirstPage, session.ViewerPrevPage, session.ViewerNextPage, session.ViewerLastPage:
		m.handlePageChange(ctx, s, setting, action)
		return
	}

	// Handle different setting types
	switch setting.Type {
	case enum.SettingTypeID:
		m.handleIDModal(ctx, setting)
	case enum.SettingTypeNumber:
		m.handleNumberModal(ctx, s, setting)
	case enum.SettingTypeText:
		m.handleTextModal(ctx, s, setting)
	case enum.SettingTypeBool, enum.SettingTypeEnum:
		m.layout.logger.Error("Button change not supported for this setting type",
			zap.String("type", setting.Type.String()))

		return
	}
}

// handleIDModal handles the modal for ID type settings.
func (m *UpdateMenu) handleIDModal(ctx *interaction.Context, setting *session.Setting) {
	modal := discord.NewModalCreateBuilder().
		SetCustomID(setting.Key).
		SetTitle("Toggle " + setting.Name).
		AddActionRow(
			discord.NewTextInput("0", discord.TextInputStyleParagraph, setting.Name).
				WithRequired(true).
				WithPlaceholder("Enter the user ID to toggle...").
				WithMaxLength(128),
		)

	ctx.Modal(modal)
}

// handleNumberModal handles the modal for number type settings.
func (m *UpdateMenu) handleNumberModal(ctx *interaction.Context, s *session.Session, setting *session.Setting) {
	modal := discord.NewModalCreateBuilder().
		SetCustomID(setting.Key).
		SetTitle("Set " + setting.Name).
		AddActionRow(
			discord.NewTextInput("0", discord.TextInputStyleParagraph, setting.Name).
				WithRequired(true).
				WithPlaceholder("Enter a number...").
				WithMaxLength(128).
				WithValue(session.SettingDisplay.Get(s)),
		)

	ctx.Modal(modal)
}

// handleTextModal handles the modal for text type settings.
func (m *UpdateMenu) handleTextModal(ctx *interaction.Context, s *session.Session, setting *session.Setting) {
	modal := discord.NewModalCreateBuilder().
		SetCustomID(setting.Key).
		SetTitle("Set " + setting.Name).
		AddActionRow(
			discord.NewTextInput("0", discord.TextInputStyleParagraph, setting.Name).
				WithRequired(true).
				WithPlaceholder("Enter your description...").
				WithMaxLength(512).
				WithValue(session.SettingDisplay.Get(s)),
		)

	ctx.Modal(modal)
}

// handlePageChange handles pagination for ID and text type settings.
func (m *UpdateMenu) handlePageChange(
	ctx *interaction.Context, s *session.Session, setting *session.Setting, action session.ViewerAction,
) {
	// Calculate pagination first
	if !m.calculatePagination(s) {
		return
	}

	// Calculate offset for the current page
	switch setting.Type {
	case enum.SettingTypeID:
		// Get current state
		totalPages := session.PaginationTotalPages.Get(s)

		// Handle navigation action
		newPage := action.ParsePageAction(s, totalPages)
		offset := newPage * constants.SettingsIDsPerPage

		// Update session state
		session.PaginationOffset.Set(s, offset)
		session.PaginationPage.Set(s, newPage)
		ctx.Reload("")

	case enum.SettingTypeBool, enum.SettingTypeEnum, enum.SettingTypeNumber, enum.SettingTypeText:
		return
	}
}

// handleSettingModal processes modal submissions.
func (m *UpdateMenu) handleSettingModal(ctx *interaction.Context, s *session.Session) {
	settingType := session.SettingType.Get(s)
	settingKey := session.SettingCustomID.Get(s)
	setting := m.getSetting(settingType, settingKey)

	// Get all inputs from the modal
	var inputs []string

	for i := 0; ; i++ {
		input := ctx.Event().ModalData().Text(strconv.Itoa(i))
		if input == "" {
			break
		}

		inputs = append(inputs, input)
	}

	// Validate each input using the setting's validators
	for _, input := range inputs {
		if err := m.validateSettingValue(s, setting, input); err != nil {
			ctx.Cancel(fmt.Sprintf("Failed to validate setting value: %v", err))
			return
		}
	}

	// Update the setting using ValueUpdater with the customID and inputs
	if err := setting.ValueUpdater(ctx.Event().CustomID(), inputs, s); err != nil {
		ctx.Error(fmt.Sprintf("Failed to update setting: %v", err))
		return
	}

	// Show updated settings
	ctx.Reload("")
}

// getSetting returns the setting definition for the given type and key.
func (m *UpdateMenu) getSetting(settingType, settingKey string) *session.Setting {
	if strings.HasPrefix(settingType, constants.UserSettingPrefix) {
		return m.layout.registry.UserSettings[settingKey]
	}

	return m.layout.registry.BotSettings[settingKey]
}

// validateSettingValue validates a setting value.
func (m *UpdateMenu) validateSettingValue(s *session.Session, setting *session.Setting, value string) error {
	for _, validator := range setting.Validators {
		if err := validator(value, session.UserID.Get(s)); err != nil {
			return err
		}
	}

	return nil
}

// calculatePagination calculates and stores pagination state in the session.
func (m *UpdateMenu) calculatePagination(s *session.Session) bool {
	setting := session.SettingValue.Get(s)

	// Get total items based on setting type
	switch setting.Type {
	case enum.SettingTypeID:
		var totalItems int

		// Get the correct list of IDs based on the setting key
		switch setting.Key {
		case constants.ReviewerIDsOption:
			totalItems = len(session.BotReviewerIDs.Get(s))
		case constants.AdminIDsOption:
			totalItems = len(session.BotAdminIDs.Get(s))
		default:
			m.layout.logger.Error("Invalid setting type for pagination",
				zap.String("type", setting.Type.String()))

			return false
		}

		// Calculate total pages for ID type settings
		totalPages := max((totalItems+constants.SettingsIDsPerPage-1)/constants.SettingsIDsPerPage, 1)

		// Store pagination state in session
		session.PaginationTotalItems.Set(s, totalItems)
		session.PaginationTotalPages.Set(s, totalPages)

		return true

	case enum.SettingTypeBool, enum.SettingTypeEnum, enum.SettingTypeNumber, enum.SettingTypeText:
		return false
	}

	return false
}

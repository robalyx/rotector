package setting

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/robalyx/rotector/internal/bot/builder/setting"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/pagination"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/interfaces"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
	"go.uber.org/zap"
)

// UpdateMenu handles the interface for changing individual settings.
type UpdateMenu struct {
	layout *Layout
	page   *pagination.Page
}

// NewMenu creates a Menu and sets up its page with message builders and
// interaction handlers for changing settings.
func NewUpdateMenu(l *Layout) *UpdateMenu {
	m := &UpdateMenu{layout: l}
	m.page = &pagination.Page{
		Name: constants.SettingUpdatePageName,
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return setting.NewUpdateBuilder(s).Build()
		},
		SelectHandlerFunc: m.handleSettingChange,
		ButtonHandlerFunc: m.handleSettingButton,
		ModalHandlerFunc:  m.handleSettingModal,
	}
	return m
}

// Show prepares and displays the settings change interface.
func (m *UpdateMenu) Show(event interfaces.CommonEvent, s *session.Session, settingType, settingKey string) {
	// Get the setting definition
	setting := m.getSetting(settingType, settingKey)

	// Store setting information in session
	session.SettingName.Set(s, setting.Name)
	session.SettingType.Set(s, settingType)
	session.SettingCustomID.Set(s, settingKey)
	session.SettingValue.Set(s, setting)
	session.PaginationPage.Set(s, 0)
	session.PaginationOffset.Set(s, 0)
	_ = m.calculatePagination(s)

	// Get current value based on setting type
	currentValue := setting.ValueGetter(s)
	session.SettingDisplay.Set(s, currentValue)

	m.layout.paginationManager.NavigateTo(event, s, m.page, "")
}

// handleSettingChange processes setting value changes.
func (m *UpdateMenu) handleSettingChange(event *events.ComponentInteractionCreate, s *session.Session, customID string, option string) {
	settingType := session.SettingType.Get(s)
	settingKey := session.SettingCustomID.Get(s)
	setting := m.getSetting(settingType, settingKey)

	// Special handling for API key settings
	if setting.Type == enum.SettingTypeAPIKey {
		m.handleAPIKeyModal(event, option)
		return
	}

	// Validate the new value
	if err := m.validateSettingValue(s, setting, option); err != nil {
		m.layout.paginationManager.NavigateTo(event, s, m.page, fmt.Sprintf("Failed to validate setting value: %v", err))
		return
	}

	// Update the setting
	if err := setting.ValueUpdater(customID, []string{option}, s); err != nil {
		m.layout.paginationManager.NavigateTo(event, s, m.page, fmt.Sprintf("Failed to update setting: %v", err))
		return
	}

	m.Show(event, s, settingType, settingKey)
}

// handleSettingButton processes button interactions.
func (m *UpdateMenu) handleSettingButton(event *events.ComponentInteractionCreate, s *session.Session, customID string) {
	// Handle back button
	split := strings.Split(customID, "_")
	if len(split) > 1 && split[1] == constants.BackButtonCustomID {
		m.layout.paginationManager.NavigateBack(event, s, "")
		return
	}

	// Get the current setting
	setting := session.SettingValue.Get(s)

	// Handle pagination buttons
	switch customID {
	case string(session.ViewerFirstPage), string(session.ViewerPrevPage),
		string(session.ViewerNextPage), string(session.ViewerLastPage):
		m.handlePageChange(event, s, setting, customID)
		return
	}

	// Handle different setting types
	switch setting.Type {
	case enum.SettingTypeID:
		m.handleIDModal(event, s, setting)
	case enum.SettingTypeNumber:
		m.handleNumberModal(event, s, setting)
	case enum.SettingTypeText:
		m.handleTextModal(event, s, setting)
	case enum.SettingTypeBool, enum.SettingTypeEnum, enum.SettingTypeAPIKey:
		m.layout.logger.Error("Button change not supported for this setting type",
			zap.String("type", setting.Type.String()))
		return
	}
}

// handleIDModal handles the modal for ID type settings.
func (m *UpdateMenu) handleIDModal(event *events.ComponentInteractionCreate, _ *session.Session, setting *session.Setting) {
	textInput := discord.NewTextInput("0", discord.TextInputStyleParagraph, setting.Name).
		WithRequired(true).
		WithPlaceholder("Enter the user ID to toggle...")

	modal := discord.NewModalCreateBuilder().
		SetCustomID(setting.Key).
		SetTitle("Toggle " + setting.Name).
		AddActionRow(textInput)

	if err := event.Modal(modal.Build()); err != nil {
		m.layout.logger.Error("Failed to open the ID input form", zap.Error(err))
		m.layout.paginationManager.RespondWithError(event, "Failed to open the form. Please try again.")
	}
}

// handleNumberModal handles the modal for number type settings.
func (m *UpdateMenu) handleNumberModal(event *events.ComponentInteractionCreate, s *session.Session, setting *session.Setting) {
	textInput := discord.NewTextInput("0", discord.TextInputStyleParagraph, setting.Name).
		WithRequired(true).
		WithPlaceholder("Enter a number...").
		WithValue(session.SettingDisplay.Get(s))

	modal := discord.NewModalCreateBuilder().
		SetCustomID(setting.Key).
		SetTitle("Set " + setting.Name).
		AddActionRow(textInput)

	if err := event.Modal(modal.Build()); err != nil {
		m.layout.logger.Error("Failed to open the number input form", zap.Error(err))
		m.layout.paginationManager.RespondWithError(event, "Failed to open the form. Please try again.")
	}
}

// handleTextModal handles the modal for text type settings.
func (m *UpdateMenu) handleTextModal(event *events.ComponentInteractionCreate, s *session.Session, setting *session.Setting) {
	textInput := discord.NewTextInput("0", discord.TextInputStyleParagraph, setting.Name).
		WithRequired(true).
		WithPlaceholder("Enter your description...").
		WithValue(session.SettingDisplay.Get(s))

	modal := discord.NewModalCreateBuilder().
		SetCustomID(setting.Key).
		SetTitle("Set " + setting.Name).
		AddActionRow(textInput)

	if err := event.Modal(modal.Build()); err != nil {
		m.layout.logger.Error("Failed to open the text input form", zap.Error(err))
		m.layout.paginationManager.RespondWithError(event, "Failed to open the form. Please try again.")
	}
}

// handleAPIKeyModal handles the modal for API key type settings.
func (m *UpdateMenu) handleAPIKeyModal(event *events.ComponentInteractionCreate, option string) {
	var modalTitle string
	var inputs []discord.TextInputComponent

	switch option {
	case constants.APIKeyCreateIDOption:
		inputs = []discord.TextInputComponent{
			discord.NewTextInput("0", discord.TextInputStyleParagraph, "Key Description").
				WithPlaceholder("Enter a description for the new API key...").
				WithRequired(true),
		}
		modalTitle = "Create New API Key"

	case constants.APIKeyDeleteIDOption:
		inputs = []discord.TextInputComponent{
			discord.NewTextInput("0", discord.TextInputStyleParagraph, "API Key").
				WithPlaceholder("Enter the API key to delete...").
				WithRequired(true),
		}
		modalTitle = "Delete API Key"

	case constants.APIKeyUpdateIDOption:
		inputs = []discord.TextInputComponent{
			discord.NewTextInput("0", discord.TextInputStyleParagraph, "API Key").
				WithPlaceholder("Enter the API key to update...").
				WithRequired(true),
			discord.NewTextInput("1", discord.TextInputStyleParagraph, "New Description").
				WithPlaceholder("Enter the new description...").
				WithRequired(true),
		}
		modalTitle = "Update API Key Description"

	default:
		m.layout.paginationManager.RespondWithError(event, "Invalid API key action")
		return
	}

	// Create the modal builder
	builder := discord.NewModalCreateBuilder().
		SetCustomID(option).
		SetTitle(modalTitle)

	// Add each text input in its own action row
	for _, input := range inputs {
		builder.AddActionRow(input)
	}

	if err := event.Modal(builder.Build()); err != nil {
		m.layout.logger.Error("Failed to open the API key form", zap.Error(err))
		m.layout.paginationManager.RespondWithError(event, "Failed to open the form. Please try again.")
	}
}

// handlePageChange handles pagination for ID and text type settings.
func (m *UpdateMenu) handlePageChange(event *events.ComponentInteractionCreate, s *session.Session, setting *session.Setting, customID string) {
	// Calculate pagination first
	if !m.calculatePagination(s) {
		return
	}

	// Get current state
	totalPages := session.PaginationTotalPages.Get(s)

	// Handle navigation action
	action := session.ViewerAction(customID)
	newPage := action.ParsePageAction(s, action, totalPages-1)

	// Update session state
	session.PaginationPage.Set(s, newPage)

	// Calculate offset for the current page
	var itemsPerPage int
	switch setting.Type {
	case enum.SettingTypeID:
		itemsPerPage = constants.SettingsIDsPerPage
	case enum.SettingTypeAPIKey:
		itemsPerPage = constants.SettingsKeysPerPage
	case enum.SettingTypeBool, enum.SettingTypeEnum, enum.SettingTypeNumber, enum.SettingTypeText:
		return
	}
	offset := newPage * itemsPerPage
	session.PaginationOffset.Set(s, offset)

	// Refresh the display
	m.layout.paginationManager.NavigateTo(event, s, m.page, "")
}

// handleSettingModal processes modal submissions.
func (m *UpdateMenu) handleSettingModal(event *events.ModalSubmitInteractionCreate, s *session.Session) {
	settingType := session.SettingType.Get(s)
	settingKey := session.SettingCustomID.Get(s)
	setting := m.getSetting(settingType, settingKey)

	// Get all inputs from the modal
	var inputs []string
	for i := 0; ; i++ {
		input := event.Data.Text(strconv.Itoa(i))
		if input == "" {
			break
		}
		inputs = append(inputs, input)
	}

	// Validate each input using the setting's validators
	for _, input := range inputs {
		if err := m.validateSettingValue(s, setting, input); err != nil {
			m.layout.paginationManager.NavigateTo(event, s, m.page, fmt.Sprintf("Failed to validate setting value: %v", err))
			return
		}
	}

	// Update the setting using ValueUpdater with the customID and inputs
	if err := setting.ValueUpdater(event.Data.CustomID, inputs, s); err != nil {
		m.layout.paginationManager.NavigateTo(event, s, m.page, fmt.Sprintf("Failed to update setting: %v", err))
		return
	}

	// Show updated settings
	m.Show(event, s, settingType, settingKey)
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
		if err := validator(value, s.UserID()); err != nil {
			return err
		}
	}
	return nil
}

// calculatePagination calculates and stores pagination state in the session.
func (m *UpdateMenu) calculatePagination(s *session.Session) bool {
	var totalItems int
	var itemsPerPage int

	setting := session.SettingValue.Get(s)

	// Get total items based on setting type
	switch setting.Type {
	case enum.SettingTypeID:
		switch setting.Key {
		case constants.ReviewerIDsOption:
			totalItems = len(session.BotReviewerIDs.Get(s))
		case constants.AdminIDsOption:
			totalItems = len(session.BotAdminIDs.Get(s))
		}
		itemsPerPage = constants.SettingsIDsPerPage

	case enum.SettingTypeAPIKey:
		totalItems = len(session.BotAPIKeys.Get(s))
		itemsPerPage = constants.SettingsKeysPerPage

	case enum.SettingTypeBool, enum.SettingTypeEnum, enum.SettingTypeNumber, enum.SettingTypeText:
		return false
	}

	// Calculate total pages
	totalPages := (totalItems + itemsPerPage - 1) / itemsPerPage
	if totalPages < 1 {
		totalPages = 1
	}

	// Store pagination state in session
	session.PaginationTotalItems.Set(s, totalItems)
	session.PaginationTotalPages.Set(s, totalPages)
	return true
}

package setting

import (
	"context"
	"strings"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/rotector/rotector/internal/bot/builder/setting"
	"github.com/rotector/rotector/internal/bot/constants"
	"github.com/rotector/rotector/internal/bot/core/pagination"
	"github.com/rotector/rotector/internal/bot/core/session"
	"github.com/rotector/rotector/internal/bot/interfaces"
	"github.com/rotector/rotector/internal/common/storage/database/models"
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
		Name: "Setting Change Menu",
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
	s.Set(constants.SessionKeySettingName, setting.Name)
	s.Set(constants.SessionKeySettingType, settingType)
	s.Set(constants.SessionKeyCustomID, settingKey)
	s.Set(constants.SessionKeySetting, setting)

	// Get current value based on setting type
	var userSettings *models.UserSetting
	s.GetInterface(constants.SessionKeyUserSettings, &userSettings)
	var botSettings *models.BotSetting
	s.GetInterface(constants.SessionKeyBotSettings, &botSettings)

	currentValue := setting.ValueGetter(userSettings, botSettings)
	s.Set(constants.SessionKeyCurrentValue, currentValue)

	m.layout.paginationManager.NavigateTo(event, s, m.page, "")
}

// handleSettingChange processes setting value changes.
func (m *UpdateMenu) handleSettingChange(event *events.ComponentInteractionCreate, s *session.Session, _ string, option string) {
	settingType := s.GetString(constants.SessionKeySettingType)
	settingKey := s.GetString(constants.SessionKeyCustomID)
	setting := m.getSetting(settingType, settingKey)

	// Validate the new value
	if err := m.validateSettingValue(s, setting, option); err != nil {
		m.layout.paginationManager.RespondWithError(event, err.Error())
		return
	}

	// Update the setting
	if err := m.updateSetting(s, setting, option); err != nil {
		m.layout.paginationManager.RespondWithError(event, "Failed to update setting")
		return
	}

	m.Show(event, s, settingType, settingKey)
}

// validateSettingValue validates a setting value.
func (m *UpdateMenu) validateSettingValue(s *session.Session, setting models.Setting, value string) error {
	userID := s.GetUint64(constants.SessionKeyUserID)

	for _, validator := range setting.Validators {
		if err := validator(value, userID); err != nil {
			return err
		}
	}
	return nil
}

// updateSetting updates a setting value in the database.
func (m *UpdateMenu) updateSetting(s *session.Session, setting models.Setting, value string) error {
	var userSettings *models.UserSetting
	s.GetInterface(constants.SessionKeyUserSettings, &userSettings)
	var botSettings *models.BotSetting
	s.GetInterface(constants.SessionKeyBotSettings, &botSettings)

	// Use the setting's ValueUpdater to update the value
	if err := setting.ValueUpdater(value, userSettings, botSettings); err != nil {
		return err
	}

	// Save to database based on setting type
	if strings.HasPrefix(s.GetString(constants.SessionKeySettingType), constants.UserSettingPrefix) {
		err := m.layout.db.Settings().SaveUserSettings(context.Background(), userSettings)
		if err != nil {
			return err
		}
		s.Set(constants.SessionKeyUserSettings, userSettings)
	} else {
		err := m.layout.db.Settings().SaveBotSettings(context.Background(), botSettings)
		if err != nil {
			return err
		}
		s.Set(constants.SessionKeyBotSettings, botSettings)
	}

	return nil
}

// handleSettingButton processes button interactions.
func (m *UpdateMenu) handleSettingButton(event *events.ComponentInteractionCreate, s *session.Session, customID string) {
	// Handle back button
	split := strings.Split(customID, "_")
	if len(split) > 1 && split[1] == constants.BackButtonCustomID {
		settingType := split[0]
		if strings.HasPrefix(settingType, constants.UserSettingPrefix) {
			m.layout.userMenu.Show(event, s)
		} else if strings.HasPrefix(settingType, constants.BotSettingPrefix) {
			m.layout.botMenu.Show(event, s)
		}
		return
	}

	// Get the current setting
	var setting models.Setting
	s.GetInterface(constants.SessionKeySetting, &setting)

	// Handle ID and number setting button clicks
	if setting.Type == models.SettingTypeID || setting.Type == models.SettingTypeNumber || setting.Type == models.SettingTypeText {
		textInput := discord.NewTextInput(string(setting.Type), discord.TextInputStyleParagraph, setting.Name).WithRequired(true)
		var modalTitle string

		switch setting.Type {
		case models.SettingTypeID:
			textInput.WithPlaceholder("Enter the user ID to toggle...")
			modalTitle = "Toggle " + setting.Name
		case models.SettingTypeNumber:
			textInput.WithPlaceholder("Enter a number...").
				WithValue(s.GetString(constants.SessionKeyCurrentValue))
			modalTitle = "Set " + setting.Name
		case models.SettingTypeText:
			textInput.WithPlaceholder("Enter your message...").
				WithValue(s.GetString(constants.SessionKeyCurrentValue)).
				WithStyle(discord.TextInputStyleParagraph)
			modalTitle = "Set " + setting.Name
		} //exhaustive:ignore

		modal := discord.NewModalCreateBuilder().
			SetCustomID(setting.Key).
			SetTitle(modalTitle).
			AddActionRow(textInput)

		// Show modal to user
		if err := event.Modal(modal.Build()); err != nil {
			m.layout.logger.Error("Failed to open the input form", zap.Error(err))
			m.layout.paginationManager.RespondWithError(event, "Failed to open the input form. Please try again.")
		}
	}
}

// handleSettingModal processes modal submissions.
func (m *UpdateMenu) handleSettingModal(event *events.ModalSubmitInteractionCreate, s *session.Session) {
	settingType := s.GetString(constants.SessionKeySettingType)
	settingKey := s.GetString(constants.SessionKeyCustomID)
	setting := m.getSetting(settingType, settingKey)

	// Get input from modal
	input := event.Data.Text(string(setting.Type))

	// Validate the input using the setting's validators
	if err := m.validateSettingValue(s, setting, input); err != nil {
		m.layout.paginationManager.RespondWithError(event, err.Error())
		return
	}

	// Update the setting using ValueUpdater
	if err := m.updateSetting(s, setting, input); err != nil {
		m.layout.paginationManager.RespondWithError(event, "Failed to update setting")
		return
	}

	// Show updated settings
	m.Show(event, s, settingType, settingKey)
}

// getSetting returns the setting definition for the given type and key.
func (m *UpdateMenu) getSetting(settingType, settingKey string) models.Setting {
	if strings.HasPrefix(settingType, constants.UserSettingPrefix) {
		return m.layout.registry.UserSettings[settingKey]
	}
	return m.layout.registry.BotSettings[settingKey]
}
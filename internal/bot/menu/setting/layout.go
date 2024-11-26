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

// Menu handles the interface for changing individual settings.
type Menu struct {
	handler *Handler
	page    *pagination.Page
}

// NewMenu creates a Menu and sets up its page with message builders and
// interaction handlers for changing settings.
func NewMenu(h *Handler) *Menu {
	m := &Menu{handler: h}
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

// ShowMenu prepares and displays the settings change interface.
func (m *Menu) ShowMenu(event interfaces.CommonEvent, s *session.Session, settingType, settingKey string) {
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

	m.handler.paginationManager.NavigateTo(event, s, m.page, "")
}

// handleSettingChange processes setting value changes.
func (m *Menu) handleSettingChange(event *events.ComponentInteractionCreate, s *session.Session, _ string, option string) {
	settingType := s.GetString(constants.SessionKeySettingType)
	settingKey := s.GetString(constants.SessionKeyCustomID)
	setting := m.getSetting(settingType, settingKey)

	// Validate the new value
	if err := m.validateSettingValue(s, setting, option); err != nil {
		m.handler.paginationManager.RespondWithError(event, err.Error())
		return
	}

	// Update the setting
	if err := m.updateSetting(s, setting, option); err != nil {
		m.handler.paginationManager.RespondWithError(event, "Failed to update setting")
		return
	}

	m.ShowMenu(event, s, settingType, settingKey)
}

// validateSettingValue validates a setting value.
func (m *Menu) validateSettingValue(s *session.Session, setting models.Setting, value string) error {
	userID := s.GetUint64(constants.SessionKeyUserID)

	for _, validator := range setting.Validators {
		if err := validator(value, userID); err != nil {
			return err
		}
	}
	return nil
}

// updateSetting updates a setting value in the database.
func (m *Menu) updateSetting(s *session.Session, setting models.Setting, value string) error {
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
		err := m.handler.db.Settings().SaveUserSettings(context.Background(), userSettings)
		if err != nil {
			return err
		}
		s.Set(constants.SessionKeyUserSettings, userSettings)
	} else {
		err := m.handler.db.Settings().SaveBotSettings(context.Background(), botSettings)
		if err != nil {
			return err
		}
		s.Set(constants.SessionKeyBotSettings, botSettings)
	}

	return nil
}

// handleSettingButton processes button interactions.
func (m *Menu) handleSettingButton(event *events.ComponentInteractionCreate, s *session.Session, customID string) {
	// Handle back button
	split := strings.Split(customID, "_")
	if len(split) > 1 && split[1] == constants.BackButtonCustomID {
		settingType := split[0]
		if strings.HasPrefix(settingType, constants.UserSettingPrefix) {
			m.handler.userMenu.ShowMenu(event, s)
		} else if strings.HasPrefix(settingType, constants.BotSettingPrefix) {
			m.handler.botMenu.ShowMenu(event, s)
		}
		return
	}

	// Get the current setting
	var setting models.Setting
	s.GetInterface(constants.SessionKeySetting, &setting)

	// Handle ID and number setting button clicks
	if setting.Type == models.SettingTypeID || setting.Type == models.SettingTypeNumber {
		textInput := discord.NewTextInput(string(setting.Type), discord.TextInputStyleShort, setting.Name).WithRequired(true)
		var modalTitle string

		if setting.Type == models.SettingTypeID {
			textInput.WithPlaceholder("Enter the user ID to toggle...")
			modalTitle = "Toggle " + setting.Name
		} else {
			textInput.WithPlaceholder("Enter a number...").
				WithValue(s.GetString(constants.SessionKeyCurrentValue))
			modalTitle = "Set " + setting.Name
		}

		modal := discord.NewModalCreateBuilder().
			SetCustomID(setting.Key).
			SetTitle(modalTitle).
			AddActionRow(textInput)

		// Show modal to user
		if err := event.Modal(modal.Build()); err != nil {
			m.handler.logger.Error("Failed to open the input form", zap.Error(err))
			m.handler.paginationManager.RespondWithError(event, "Failed to open the input form. Please try again.")
		}
	}
}

// handleSettingModal processes modal submissions.
func (m *Menu) handleSettingModal(event *events.ModalSubmitInteractionCreate, s *session.Session) {
	settingType := s.GetString(constants.SessionKeySettingType)
	settingKey := s.GetString(constants.SessionKeyCustomID)
	setting := m.getSetting(settingType, settingKey)

	// Get input from modal
	input := event.Data.Text(string(setting.Type))

	// Validate the input using the setting's validators
	if err := m.validateSettingValue(s, setting, input); err != nil {
		m.handler.paginationManager.RespondWithError(event, err.Error())
		return
	}

	// Update the setting using ValueUpdater
	if err := m.updateSetting(s, setting, input); err != nil {
		m.handler.paginationManager.RespondWithError(event, "Failed to update setting")
		return
	}

	// Show updated settings
	m.ShowMenu(event, s, settingType, settingKey)
}

// getSetting returns the setting definition for the given type and key.
func (m *Menu) getSetting(settingType, settingKey string) models.Setting {
	if strings.HasPrefix(settingType, constants.UserSettingPrefix) {
		return m.handler.registry.UserSettings[settingKey]
	}
	return m.handler.registry.BotSettings[settingKey]
}

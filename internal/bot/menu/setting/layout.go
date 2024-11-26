package setting

import (
	"context"
	"fmt"
	"strconv"
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
	handler  *Handler
	page     *pagination.Page
	registry *models.SettingRegistry
}

// NewMenu creates a Menu and sets up its page with message builders and
// interaction handlers for changing settings.
func NewMenu(h *Handler) *Menu {
	m := &Menu{
		handler:  h,
		registry: models.NewSettingRegistry(),
	}
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
	var setting models.Setting
	if strings.HasPrefix(settingType, constants.UserSettingPrefix) {
		setting = m.registry.UserSettings[settingKey]
	} else {
		setting = m.registry.BotSettings[settingKey]
	}

	// Store setting information in session
	s.Set(constants.SessionKeySettingName, setting.Name)
	s.Set(constants.SessionKeySettingType, settingType)
	s.Set(constants.SessionKeyCustomID, settingKey)
	s.Set(constants.SessionKeySetting, setting)

	// Get current value based on setting type
	currentValue := m.getCurrentValue(s, setting, settingType)
	s.Set(constants.SessionKeyCurrentValue, currentValue)

	m.handler.paginationManager.NavigateTo(event, s, m.page, "")
}

// getCurrentValue gets the current value of a setting.
func (m *Menu) getCurrentValue(s *session.Session, setting models.Setting, settingType string) string {
	// Handle user settings
	if strings.HasPrefix(settingType, constants.UserSettingPrefix) {
		var userSettings *models.UserSetting
		s.GetInterface(constants.SessionKeyUserSettings, &userSettings)

		switch setting.Key {
		case constants.StreamerModeOption:
			return strconv.FormatBool(userSettings.StreamerMode)
		case constants.UserDefaultSortOption:
			return userSettings.UserDefaultSort
		case constants.GroupDefaultSortOption:
			return userSettings.GroupDefaultSort
		case constants.ReviewModeOption:
			return models.FormatReviewMode(userSettings.ReviewMode)
		}
	}

	// Handle bot settings
	if strings.HasPrefix(settingType, constants.BotSettingPrefix) {
		var botSettings *models.BotSetting
		s.GetInterface(constants.SessionKeyBotSettings, &botSettings)

		switch setting.Key {
		case constants.ReviewerIDsOption:
			return fmt.Sprintf("%v", botSettings.ReviewerIDs)
		case constants.AdminIDsOption:
			return fmt.Sprintf("%v", botSettings.AdminIDs)
		}
	}

	return fmt.Sprintf("%v", setting.DefaultValue)
}

// handleSettingChange processes setting value changes.
func (m *Menu) handleSettingChange(event *events.ComponentInteractionCreate, s *session.Session, _ string, option string) {
	settingType := s.GetString(constants.SessionKeySettingType)
	settingKey := s.GetString(constants.SessionKeyCustomID)

	var setting models.Setting
	if strings.HasPrefix(settingType, constants.UserSettingPrefix) {
		setting = m.registry.UserSettings[settingKey]
	} else {
		setting = m.registry.BotSettings[settingKey]
	}

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

	switch setting.Key {
	case constants.StreamerModeOption:
		boolVal, _ := strconv.ParseBool(value)
		userSettings.StreamerMode = boolVal
	case constants.UserDefaultSortOption:
		userSettings.UserDefaultSort = value
	case constants.GroupDefaultSortOption:
		userSettings.GroupDefaultSort = value
	case constants.ReviewModeOption:
		userSettings.ReviewMode = value
	}

	// Save to database
	err := m.handler.db.Settings().SaveUserSettings(context.Background(), userSettings)
	if err != nil {
		return err
	}

	// Update session
	s.Set(constants.SessionKeyUserSettings, userSettings)
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

	// Handle ID setting button click
	if setting.Type == models.SettingTypeID {
		textInput := discord.NewTextInput("id_input", discord.TextInputStyleShort, "User ID").
			WithRequired(true).
			WithPlaceholder("Enter the user ID to toggle...")

		modal := discord.NewModalCreateBuilder().
			SetCustomID(setting.Key).
			SetTitle("Toggle " + setting.Name).
			AddActionRow(textInput)

		// Show modal to user
		if err := event.Modal(modal.Build()); err != nil {
			m.handler.logger.Error("Failed to open the ID input form", zap.Error(err))
			m.handler.paginationManager.RespondWithError(event, "Failed to open the ID input form. Please try again.")
		}
	}
}

// handleSettingModal processes modal submissions.
func (m *Menu) handleSettingModal(event *events.ModalSubmitInteractionCreate, s *session.Session) {
	settingType := s.GetString(constants.SessionKeySettingType)
	settingKey := s.GetString(constants.SessionKeyCustomID)

	var setting models.Setting
	if strings.HasPrefix(settingType, constants.UserSettingPrefix) {
		setting = m.registry.UserSettings[settingKey]
	} else {
		setting = m.registry.BotSettings[settingKey]
	}

	// Get ID input from modal
	idStr := event.Data.Text("id_input")

	// Validate the input using the setting's validators
	if err := m.validateSettingValue(s, setting, idStr); err != nil {
		m.handler.paginationManager.RespondWithError(event, err.Error())
		return
	}

	// Parse the ID after validation
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		m.handler.paginationManager.RespondWithError(event, "Invalid ID format")
		return
	}

	// Handle different ID settings
	var botSettings *models.BotSetting
	s.GetInterface(constants.SessionKeyBotSettings, &botSettings)

	switch setting.Key {
	case constants.ReviewerIDsOption:
		if err := m.handler.db.Settings().ToggleReviewerID(context.Background(), id); err != nil {
			m.handler.paginationManager.RespondWithError(event, "Failed to toggle reviewer ID")
			return
		}
	case constants.AdminIDsOption:
		if err := m.handler.db.Settings().ToggleAdminID(context.Background(), id); err != nil {
			m.handler.paginationManager.RespondWithError(event, "Failed to toggle admin ID")
			return
		}
	}

	// Refresh bot settings in session
	updatedSettings, err := m.handler.db.Settings().GetBotSettings(context.Background())
	if err != nil {
		m.handler.paginationManager.RespondWithError(event, "Failed to refresh settings")
		return
	}
	s.Set(constants.SessionKeyBotSettings, updatedSettings)

	// Show updated settings
	m.ShowMenu(event, s, settingType, settingKey)
}

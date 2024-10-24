package settings

import (
	"strconv"
	"strings"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/rotector/rotector/internal/bot/constants"
	"github.com/rotector/rotector/internal/bot/handlers/settings/builders"
	"github.com/rotector/rotector/internal/bot/interfaces"
	"github.com/rotector/rotector/internal/bot/pagination"
	"github.com/rotector/rotector/internal/bot/session"
	"go.uber.org/zap"
)

// SettingMenu is the handler for the setting change menu.
type SettingMenu struct {
	handler *Handler
	page    *pagination.Page
}

// NewSettingMenu creates a new SettingMenu instance.
func NewSettingMenu(h *Handler) *SettingMenu {
	m := &SettingMenu{handler: h}
	m.page = &pagination.Page{
		Name: "Setting Change Menu",
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			options := s.Get(constants.SessionKeyOptions).([]discord.StringSelectMenuOption)
			return builders.NewSettingChangeBuilder(s).
				AddOptions(options...).
				Build()
		},
		SelectHandlerFunc: m.handleSettingChange,
		ButtonHandlerFunc: m.handleSettingButton,
	}
	return m
}

// ShowMenu displays the setting change menu.
func (m *SettingMenu) ShowMenu(event interfaces.CommonEvent, s *session.Session, settingName, settingType, customID string, currentValueFunc func() string, options []discord.StringSelectMenuOption) {
	s.Set(constants.SessionKeySettingName, settingName)
	s.Set(constants.SessionKeySettingType, settingType)
	s.Set(constants.SessionKeyCurrentValueFunc, currentValueFunc)
	s.Set(constants.SessionKeyCustomID, customID)
	s.Set(constants.SessionKeyOptions, options)

	m.handler.paginationManager.NavigateTo(m.page.Name, s)
	m.handler.paginationManager.UpdateMessage(event, s, m.page, "")
}

// handleSettingChange handles the select menu for the setting change menu.
func (m *SettingMenu) handleSettingChange(event *events.ComponentInteractionCreate, s *session.Session, customID string, option string) {
	settingType := s.GetString(constants.SessionKeySettingType)
	currentValueFunc := s.Get(constants.SessionKeyCurrentValueFunc).(func() string)

	// Save the setting immediately
	m.saveSetting(event, settingType, customID, option)

	// Update the current value and show the updated setting
	s.Set(constants.SessionKeyCurrentValue, currentValueFunc())
	m.handler.paginationManager.UpdateMessage(event, s, m.page, "Setting updated successfully.")
}

// handleSettingButton handles the buttons for the setting change menu.
func (m *SettingMenu) handleSettingButton(event *events.ComponentInteractionCreate, s *session.Session, customID string) {
	split := strings.Split(customID, "_")
	if len(split) > 1 && split[1] == constants.BackButtonCustomID {
		// Back to the main menu
		settingType := split[0]
		if strings.HasPrefix(settingType, constants.UserSettingPrefix) {
			m.handler.userMenu.ShowMenu(event, s)
		} else if strings.HasPrefix(settingType, constants.GuildSettingPrefix) {
			m.handler.guildMenu.ShowMenu(event, s)
		}
	}
}

// saveSetting saves the given setting to the database.
func (m *SettingMenu) saveSetting(event interfaces.CommonEvent, settingType, customID, option string) {
	switch settingType {
	case constants.UserSettingPrefix:
		m.saveUserSetting(event, customID, option)
	case constants.GuildSettingPrefix:
		m.saveGuildSetting(event, customID, option)
	default:
		m.handler.logger.Warn("unknown setting type", zap.String("settingType", settingType), zap.String("customID", customID), zap.String("option", option))
	}
}

// saveUserSetting saves a user-specific setting.
func (m *SettingMenu) saveUserSetting(event interfaces.CommonEvent, customID, option string) {
	settings, err := m.handler.db.Settings().GetUserSettings(uint64(event.User().ID))
	if err != nil {
		m.handler.logger.Error("failed to get user settings", zap.Error(err))
		return
	}

	switch customID {
	case constants.StreamerModeOption:
		if settings.StreamerMode, err = strconv.ParseBool(option); err != nil {
			m.handler.logger.Error("failed to parse streamer mode", zap.Error(err))
			return
		}
	case constants.DefaultSortOption:
		settings.DefaultSort = option
	default:
		m.handler.logger.Warn("unknown user setting", zap.String("customID", customID), zap.String("option", option))
		return
	}

	// Save the user settings
	if err := m.handler.db.Settings().SaveUserSettings(settings); err != nil {
		m.handler.logger.Error("failed to save user settings", zap.Error(err))
	}
}

// saveGuildSetting saves a guild-specific setting.
func (m *SettingMenu) saveGuildSetting(event interfaces.CommonEvent, customID, option string) {
	guildID := uint64(*event.GuildID())

	switch customID {
	case constants.WhitelistedRolesOption:
		// Parse the role ID
		roleID, err := strconv.ParseUint(option, 10, 64)
		if err != nil {
			m.handler.logger.Error("failed to parse role ID", zap.Error(err))
			return
		}

		// Toggle the whitelisted role
		if err := m.handler.db.Settings().ToggleWhitelistedRole(guildID, roleID); err != nil {
			m.handler.logger.Error("failed to toggle whitelisted role", zap.Error(err))
		}
	default:
		m.handler.logger.Warn("unknown guild setting", zap.String("customID", customID), zap.String("option", option))
	}
}

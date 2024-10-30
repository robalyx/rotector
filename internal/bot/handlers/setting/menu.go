package setting

import (
	"strconv"
	"strings"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/rotector/rotector/internal/bot/constants"
	"github.com/rotector/rotector/internal/bot/handlers/setting/builders"
	"github.com/rotector/rotector/internal/bot/interfaces"
	"github.com/rotector/rotector/internal/bot/pagination"
	"github.com/rotector/rotector/internal/bot/session"
	"go.uber.org/zap"
)

// Menu is the handler for the setting change menu.
type Menu struct {
	handler *Handler
	page    *pagination.Page
}

// NewMenu creates a new Menu instance.
func NewMenu(h *Handler) *Menu {
	m := &Menu{handler: h}
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
func (m *Menu) ShowMenu(event interfaces.CommonEvent, s *session.Session, settingName, settingType, customID string, currentValueFunc func() string, options []discord.StringSelectMenuOption) {
	s.Set(constants.SessionKeySettingName, settingName)
	s.Set(constants.SessionKeySettingType, settingType)
	s.Set(constants.SessionKeyCurrentValueFunc, currentValueFunc)
	s.Set(constants.SessionKeyCustomID, customID)
	s.Set(constants.SessionKeyOptions, options)

	m.handler.paginationManager.NavigateTo(event, s, m.page, "")
}

// handleSettingChange handles the select menu for the setting change menu.
func (m *Menu) handleSettingChange(event *events.ComponentInteractionCreate, s *session.Session, customID string, option string) {
	// Save the setting immediately
	settingType := s.GetString(constants.SessionKeySettingType)
	m.saveSetting(event, settingType, customID, option)

	m.handler.paginationManager.NavigateTo(event, s, m.page, "Setting updated successfully.")
}

// handleSettingButton handles the buttons for the setting change menu.
func (m *Menu) handleSettingButton(event *events.ComponentInteractionCreate, s *session.Session, customID string) {
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
func (m *Menu) saveSetting(event interfaces.CommonEvent, settingType, customID, option string) {
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
func (m *Menu) saveUserSetting(event interfaces.CommonEvent, customID, option string) {
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
func (m *Menu) saveGuildSetting(event interfaces.CommonEvent, customID, option string) {
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
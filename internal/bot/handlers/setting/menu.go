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
	"github.com/rotector/rotector/internal/bot/utils"
	"github.com/rotector/rotector/internal/common/database"
	"go.uber.org/zap"
)

// Menu handles the interface for changing individual settings.
// It works with both user and guild settings through a common interface.
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
			var options []discord.StringSelectMenuOption
			s.GetInterface(constants.SessionKeyOptions, &options)

			return builders.NewSettingChangeBuilder(s).
				AddOptions(options...).
				Build()
		},
		SelectHandlerFunc: m.handleSettingChange,
		ButtonHandlerFunc: m.handleSettingButton,
	}
	return m
}

// ShowMenu prepares and displays the settings change interface by loading
// the current value and available options for the selected setting.
func (m *Menu) ShowMenu(event interfaces.CommonEvent, s *session.Session, settingName, settingType, customID string) {
	// Store setting information in session for the message builder
	s.Set(constants.SessionKeySettingName, settingName)
	s.Set(constants.SessionKeySettingType, settingType)
	s.Set(constants.SessionKeyCustomID, customID)

	// Load current value and options based on setting type
	var currentValue string
	var options []discord.StringSelectMenuOption

	switch customID {
	case constants.StreamerModeOption:
		// Load user settings for streamer mode toggle
		var settings *database.UserSetting
		s.GetInterface(constants.SessionKeyUserSettings, &settings)

		currentValue = strconv.FormatBool(settings.StreamerMode)
		options = []discord.StringSelectMenuOption{
			discord.NewStringSelectMenuOption("Enable", "true"),
			discord.NewStringSelectMenuOption("Disable", "false"),
		}

	case constants.DefaultSortOption:
		// Load user settings for sort preference
		var settings *database.UserSetting
		s.GetInterface(constants.SessionKeyUserSettings, &settings)

		currentValue = settings.DefaultSort
		options = []discord.StringSelectMenuOption{
			discord.NewStringSelectMenuOption("Random", database.SortByRandom),
			discord.NewStringSelectMenuOption("Confidence", database.SortByConfidence),
			discord.NewStringSelectMenuOption("Last Updated", database.SortByLastUpdated),
		}

	case constants.WhitelistedRolesOption:
		// Load guild settings and available roles for role whitelist
		settings := m.handler.guildMenu.getGuildSettings(event)
		roles, err := event.Client().Rest().GetRoles(*event.GuildID())
		if err != nil {
			m.handler.logger.Error("Failed to fetch guild roles", zap.Error(err))
			return
		}
		currentValue = utils.FormatWhitelistedRoles(settings.WhitelistedRoles, roles)
		options = make([]discord.StringSelectMenuOption, 0, len(roles))
		for _, role := range roles {
			options = append(options, discord.NewStringSelectMenuOption(role.Name, role.ID.String()))
		}
	}

	// Store values in session for the message builder
	s.Set(constants.SessionKeyCurrentValue, currentValue)
	s.Set(constants.SessionKeyOptions, options)

	m.handler.paginationManager.NavigateTo(event, s, m.page, "")
}

// handleSettingChange processes select menu interactions by saving the new value
// and refreshing the settings display.
func (m *Menu) handleSettingChange(event *events.ComponentInteractionCreate, s *session.Session, customID string, option string) {
	settingName := s.GetString(constants.SessionKeySettingName)
	settingType := s.GetString(constants.SessionKeySettingType)
	m.saveSetting(event, s, settingType, customID, option)

	m.ShowMenu(event, s, settingName, settingType, customID)
}

// handleSettingButton processes button interactions.
func (m *Menu) handleSettingButton(event *events.ComponentInteractionCreate, s *session.Session, customID string) {
	split := strings.Split(customID, "_")
	if len(split) > 1 && split[1] == constants.BackButtonCustomID {
		// Return to the appropriate settings menu based on setting type
		settingType := split[0]
		if strings.HasPrefix(settingType, constants.UserSettingPrefix) {
			m.handler.userMenu.ShowMenu(event, s)
		} else if strings.HasPrefix(settingType, constants.GuildSettingPrefix) {
			m.handler.guildMenu.ShowMenu(event, s)
		}
	}
}

// saveSetting routes the setting change to the appropriate save function
// based on whether it's a user or guild setting.
func (m *Menu) saveSetting(event interfaces.CommonEvent, s *session.Session, settingType, customID, option string) {
	switch settingType {
	case constants.UserSettingPrefix:
		m.saveUserSetting(s, customID, option)
	case constants.GuildSettingPrefix:
		m.saveGuildSetting(event, customID, option)
	default:
		m.handler.logger.Warn("unknown setting type", zap.String("settingType", settingType))
	}
}

// saveUserSetting updates user-specific settings in both the database and session.
// It handles different setting types and validates the input before saving.
func (m *Menu) saveUserSetting(s *session.Session, customID, option string) {
	var settings *database.UserSetting
	s.GetInterface(constants.SessionKeyUserSettings, &settings)

	switch customID {
	case constants.StreamerModeOption:
		// Parse and save streamer mode toggle
		var err error
		if settings.StreamerMode, err = strconv.ParseBool(option); err != nil {
			m.handler.logger.Error("Failed to parse streamer mode", zap.Error(err))
			return
		}

	case constants.DefaultSortOption:
		// Save sort preference directly
		settings.DefaultSort = option

	default:
		m.handler.logger.Warn("Unknown user setting", zap.String("customID", customID))
		return
	}

	// Save to database and update session
	if err := m.handler.db.Settings().SaveUserSettings(settings); err != nil {
		m.handler.logger.Error("Failed to save user settings", zap.Error(err))
		return
	}

	s.Set(constants.SessionKeyUserSettings, settings)
}

// saveGuildSetting updates guild-specific settings in the database.
// It handles role whitelist changes through the toggle mechanism.
func (m *Menu) saveGuildSetting(event interfaces.CommonEvent, customID, option string) {
	guildID := uint64(*event.GuildID())

	switch customID {
	case constants.WhitelistedRolesOption:
		// Parse and toggle the selected role
		roleID, err := strconv.ParseUint(option, 10, 64)
		if err != nil {
			m.handler.logger.Error("failed to parse role ID", zap.Error(err))
			return
		}

		if err := m.handler.db.Settings().ToggleWhitelistedRole(guildID, roleID); err != nil {
			m.handler.logger.Error("failed to toggle whitelisted role", zap.Error(err))
		}

	default:
		m.handler.logger.Warn("unknown guild setting", zap.String("customID", customID))
	}
}

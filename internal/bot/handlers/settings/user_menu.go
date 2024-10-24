package settings

import (
	"strconv"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/rotector/rotector/internal/bot/constants"
	"github.com/rotector/rotector/internal/bot/handlers/settings/builders"
	"github.com/rotector/rotector/internal/bot/interfaces"
	"github.com/rotector/rotector/internal/bot/pagination"
	"github.com/rotector/rotector/internal/bot/session"
	"github.com/rotector/rotector/internal/common/database"
	"go.uber.org/zap"
)

// UserMenu is the handler for the user settings menu.
type UserMenu struct {
	handler *Handler
	page    *pagination.Page
}

// NewUserMenu creates a new UserMenu instance.
func NewUserMenu(h *Handler) *UserMenu {
	u := &UserMenu{handler: h}
	u.page = &pagination.Page{
		Name: "User Settings Menu",
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return builders.NewUserSettingsEmbed(s).Build()
		},
		SelectHandlerFunc: u.handleUserSettingSelection,
		ButtonHandlerFunc: u.handleUserSettingButton,
	}
	return u
}

// ShowMenu displays the user settings menu.
func (u *UserMenu) ShowMenu(event interfaces.CommonEvent, s *session.Session) {
	s.Set(constants.SessionKeyUserSettings, u.getUserSettings(event))

	u.handler.paginationManager.NavigateTo(u.page.Name, s)
	u.handler.paginationManager.UpdateMessage(event, s, u.page, "")
}

// handleUserSettingSelection handles the select menu for the user settings menu.
func (u *UserMenu) handleUserSettingSelection(event *events.ComponentInteractionCreate, s *session.Session, _ string, option string) {
	var settingName, settingType string
	var currentValueFunc func() string
	var options []discord.StringSelectMenuOption

	switch option {
	case constants.StreamerModeOption:
		settingName = "Streamer Mode"
		settingType = constants.UserSettingPrefix
		currentValueFunc = func() string {
			settings := u.getUserSettings(event)
			return strconv.FormatBool(settings.StreamerMode)
		}
		options = []discord.StringSelectMenuOption{
			discord.NewStringSelectMenuOption("Enable", "true"),
			discord.NewStringSelectMenuOption("Disable", "false"),
		}
	case constants.DefaultSortOption:
		settingName = "Default Sort"
		settingType = constants.UserSettingPrefix
		currentValueFunc = func() string {
			settings := u.getUserSettings(event)
			return settings.DefaultSort
		}
		options = []discord.StringSelectMenuOption{
			discord.NewStringSelectMenuOption("Random", database.SortByRandom),
			discord.NewStringSelectMenuOption("Confidence", database.SortByConfidence),
			discord.NewStringSelectMenuOption("Last Updated", database.SortByLastUpdated),
		}
	}

	u.handler.settingMenu.ShowMenu(event, s, settingName, settingType, option, currentValueFunc, options)
}

// handleUserSettingButton handles the buttons for the user settings menu.
func (u *UserMenu) handleUserSettingButton(event *events.ComponentInteractionCreate, _ *session.Session, customID string) {
	if customID == constants.BackButtonCustomID {
		u.handler.dashboardHandler.ShowDashboard(event)
	}
}

// getUserSettings fetches the user settings from the database.
func (u *UserMenu) getUserSettings(event interfaces.CommonEvent) *database.UserSetting {
	settings, err := u.handler.db.Settings().GetUserSettings(uint64(event.User().ID))
	if err != nil {
		u.handler.logger.Error("Failed to fetch user settings", zap.Error(err))
		return nil
	}
	return settings
}

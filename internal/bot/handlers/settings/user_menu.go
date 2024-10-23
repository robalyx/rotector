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
		Data: make(map[string]interface{}),
		Message: func(data map[string]interface{}) *discord.MessageUpdateBuilder {
			preferences := data["preferences"].(*database.UserPreference)
			return builders.NewUserSettingsEmbed(preferences).Build()
		},
		SelectHandlerFunc: u.handleUserSettingSelection,
		ButtonHandlerFunc: u.handleUserSettingButton,
	}
	return u
}

// ShowMenu displays the user settings menu.
func (u *UserMenu) ShowMenu(event interfaces.CommonEvent, s *session.Session) {
	u.page.Data["preferences"] = u.getUserPreferences(event)

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
			preferences := u.getUserPreferences(event)
			return strconv.FormatBool(preferences.StreamerMode)
		}
		options = []discord.StringSelectMenuOption{
			discord.NewStringSelectMenuOption("Enable", "true"),
			discord.NewStringSelectMenuOption("Disable", "false"),
		}
	case constants.DefaultSortOption:
		settingName = "Default Sort"
		settingType = constants.UserSettingPrefix
		currentValueFunc = func() string {
			preferences := u.getUserPreferences(event)
			return preferences.DefaultSort
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

// getUserPreferences fetches the user preferences from the database.
func (u *UserMenu) getUserPreferences(event interfaces.CommonEvent) *database.UserPreference {
	preferences, err := u.handler.db.Settings().GetUserPreferences(uint64(event.User().ID))
	if err != nil {
		u.handler.logger.Error("Failed to fetch user preferences", zap.Error(err))
		return nil
	}
	return preferences
}

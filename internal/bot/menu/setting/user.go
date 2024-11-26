package setting

import (
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/rotector/rotector/internal/bot/builder/setting"
	"github.com/rotector/rotector/internal/bot/constants"
	"github.com/rotector/rotector/internal/bot/core/pagination"
	"github.com/rotector/rotector/internal/bot/core/session"
	"github.com/rotector/rotector/internal/bot/interfaces"
)

// UserMenu handles the display and interaction logic for user-specific settings.
type UserMenu struct {
	handler *Handler
	page    *pagination.Page
}

// NewUserMenu creates a UserMenu and sets up its page with message builders
// and interaction handlers. The page is configured to show user settings
// and handle setting changes.
func NewUserMenu(h *Handler) *UserMenu {
	u := &UserMenu{handler: h}
	u.page = &pagination.Page{
		Name: "User Settings Menu",
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return setting.NewUserSettingsBuilder(s).Build()
		},
		SelectHandlerFunc: u.handleUserSettingSelection,
		ButtonHandlerFunc: u.handleUserSettingButton,
	}
	return u
}

// ShowMenu loads user settings from the database into the session and
// displays them through the pagination system.
func (u *UserMenu) ShowMenu(event interfaces.CommonEvent, s *session.Session) {
	u.handler.paginationManager.NavigateTo(event, s, u.page, "")
}

// handleUserSettingSelection processes select menu interactions by determining
// which setting was chosen and showing the appropriate change menu.
func (u *UserMenu) handleUserSettingSelection(event *events.ComponentInteractionCreate, s *session.Session, _ string, option string) {
	// Show the change menu for the selected setting
	u.handler.settingMenu.ShowMenu(event, s, constants.UserSettingPrefix, option)
}

// handleUserSettingButton processes button interactions.
func (u *UserMenu) handleUserSettingButton(event *events.ComponentInteractionCreate, s *session.Session, customID string) {
	if customID == constants.BackButtonCustomID {
		u.handler.dashboardHandler.ShowDashboard(event, s, "")
	}
}

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
	layout *Layout
	page   *pagination.Page
}

// NewUserMenu creates a UserMenu and sets up its page with message builders
// and interaction handlers. The page is configured to show user settings
// and handle setting changes.
func NewUserMenu(l *Layout) *UserMenu {
	m := &UserMenu{layout: l}
	m.page = &pagination.Page{
		Name: "User Settings Menu",
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return setting.NewUserSettingsBuilder(s, l.registry).Build()
		},
		SelectHandlerFunc: m.handleUserSettingSelection,
		ButtonHandlerFunc: m.handleUserSettingButton,
	}
	return m
}

// Show loads user settings from the database into the session and
// displays them through the pagination system.
func (m *UserMenu) Show(event interfaces.CommonEvent, s *session.Session) {
	m.layout.paginationManager.NavigateTo(event, s, m.page, "")
}

// handleUserSettingSelection processes select menu interactions by determining
// which setting was chosen and showing the appropriate change menu.
func (m *UserMenu) handleUserSettingSelection(event *events.ComponentInteractionCreate, s *session.Session, _ string, option string) {
	// Show the change menu for the selected setting
	m.layout.updateMenu.Show(event, s, constants.UserSettingPrefix, option)
}

// handleUserSettingButton processes button interactions.
func (m *UserMenu) handleUserSettingButton(event *events.ComponentInteractionCreate, s *session.Session, customID string) {
	if customID == constants.BackButtonCustomID {
		m.layout.paginationManager.NavigateBack(event, s, "")
	}
}

package setting

import (
	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/builder/setting"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/interaction"
	"github.com/robalyx/rotector/internal/bot/core/session"
)

// UserMenu handles the display and interaction logic for user-specific settings.
type UserMenu struct {
	layout *Layout
	page   *interaction.Page
}

// NewUserMenu creates a UserMenu and sets up its page with message builders
// and interaction handlers. The page is configured to show user settings
// and handle setting changes.
func NewUserMenu(l *Layout) *UserMenu {
	m := &UserMenu{layout: l}
	m.page = &interaction.Page{
		Name: constants.UserSettingsPageName,
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return setting.NewUserSettingsBuilder(s, l.registry).Build()
		},
		SelectHandlerFunc: m.handleUserSettingSelection,
		ButtonHandlerFunc: m.handleUserSettingButton,
	}
	return m
}

// handleUserSettingSelection processes select menu interactions.
func (m *UserMenu) handleUserSettingSelection(ctx *interaction.Context, s *session.Session, _, option string) {
	// Show the change menu for the selected setting
	session.SettingType.Set(s, constants.UserSettingPrefix)
	session.SettingCustomID.Set(s, option)
	ctx.Show(constants.SettingUpdatePageName, "")
}

// handleUserSettingButton processes button interactions.
func (m *UserMenu) handleUserSettingButton(ctx *interaction.Context, _ *session.Session, customID string) {
	switch customID {
	case constants.BackButtonCustomID:
		ctx.NavigateBack("")
	}
}

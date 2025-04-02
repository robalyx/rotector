package setting

import (
	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/interaction"
	"github.com/robalyx/rotector/internal/bot/core/session"
	view "github.com/robalyx/rotector/internal/bot/views/setting"
)

// BotMenu handles the display and interaction logic for bot-wide settings.
type BotMenu struct {
	layout *Layout
	page   *interaction.Page
}

// NewBotMenu creates a BotMenu and sets up its page.
func NewBotMenu(l *Layout) *BotMenu {
	m := &BotMenu{layout: l}
	m.page = &interaction.Page{
		Name: constants.BotSettingsPageName,
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return view.NewBotSettingsBuilder(s, l.registry).Build()
		},
		SelectHandlerFunc: m.handleBotSettingSelection,
		ButtonHandlerFunc: m.handleBotSettingButton,
	}
	return m
}

// handleBotSettingSelection processes select menu interactions.
func (m *BotMenu) handleBotSettingSelection(ctx *interaction.Context, s *session.Session, _, option string) {
	// Show the change menu for the selected setting
	session.SettingType.Set(s, constants.BotSettingPrefix)
	session.SettingCustomID.Set(s, option)
	ctx.Show(constants.SettingUpdatePageName, "")
}

// handleBotSettingButton processes button interactions.
func (m *BotMenu) handleBotSettingButton(ctx *interaction.Context, _ *session.Session, customID string) {
	switch customID {
	case constants.BackButtonCustomID:
		ctx.NavigateBack("")
	}
}

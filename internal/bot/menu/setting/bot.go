package setting

import (
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/robalyx/rotector/internal/bot/builder/setting"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/pagination"
	"github.com/robalyx/rotector/internal/bot/core/session"
)

// BotMenu handles the display and interaction logic for bot-wide settings.
type BotMenu struct {
	layout *Layout
	page   *pagination.Page
}

// NewBotMenu creates a BotMenu and sets up its page.
func NewBotMenu(l *Layout) *BotMenu {
	m := &BotMenu{layout: l}
	m.page = &pagination.Page{
		Name: constants.BotSettingsPageName,
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return setting.NewBotSettingsBuilder(s, l.registry).Build()
		},
		SelectHandlerFunc: m.handleBotSettingSelection,
		ButtonHandlerFunc: m.handleBotSettingButton,
	}
	return m
}

// handleBotSettingSelection processes select menu interactions.
func (m *BotMenu) handleBotSettingSelection(event *events.ComponentInteractionCreate, s *session.Session, r *pagination.Respond, _ string, option string) {
	// Show the change menu for the selected setting
	session.SettingType.Set(s, constants.BotSettingPrefix)
	session.SettingCustomID.Set(s, option)
	r.Show(event, s, constants.SettingUpdatePageName, "")
}

// handleBotSettingButton processes button interactions.
func (m *BotMenu) handleBotSettingButton(event *events.ComponentInteractionCreate, s *session.Session, r *pagination.Respond, customID string) {
	if customID == constants.BackButtonCustomID {
		r.NavigateBack(event, s, "")
	}
}

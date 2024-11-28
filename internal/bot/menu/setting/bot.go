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

// BotMenu handles the display and interaction logic for bot-wide settings.
type BotMenu struct {
	layout *Layout
	page   *pagination.Page
}

// NewBotMenu creates a BotMenu and sets up its page.
func NewBotMenu(l *Layout) *BotMenu {
	m := &BotMenu{layout: l}
	m.page = &pagination.Page{
		Name: "Bot Settings Menu",
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return setting.NewBotSettingsBuilder(s, l.registry).Build()
		},
		SelectHandlerFunc: m.handleBotSettingSelection,
		ButtonHandlerFunc: m.handleBotSettingButton,
	}
	return m
}

// Show loads bot settings from the database into the session and
// displays them through the pagination system.
func (m *BotMenu) Show(event interfaces.CommonEvent, s *session.Session) {
	m.layout.paginationManager.NavigateTo(event, s, m.page, "")
}

// handleBotSettingSelection processes select menu interactions.
func (m *BotMenu) handleBotSettingSelection(event *events.ComponentInteractionCreate, s *session.Session, _ string, option string) {
	// Show the change menu for the selected setting
	m.layout.updateMenu.Show(event, s, constants.BotSettingPrefix, option)
}

// handleBotSettingButton processes button interactions.
func (m *BotMenu) handleBotSettingButton(event *events.ComponentInteractionCreate, s *session.Session, customID string) {
	if customID == constants.BackButtonCustomID {
		m.layout.dashboardLayout.Show(event, s, "")
	}
}

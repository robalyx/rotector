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
	handler *Handler
	page    *pagination.Page
}

// NewBotMenu creates a BotMenu and sets up its page.
func NewBotMenu(h *Handler) *BotMenu {
	b := &BotMenu{handler: h}
	b.page = &pagination.Page{
		Name: "Bot Settings Menu",
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return setting.NewBotSettingsBuilder(s).Build()
		},
		SelectHandlerFunc: b.handleBotSettingSelection,
		ButtonHandlerFunc: b.handleBotSettingButton,
	}
	return b
}

// ShowMenu loads bot settings from the database into the session and
// displays them through the pagination system.
func (b *BotMenu) ShowMenu(event interfaces.CommonEvent, s *session.Session) {
	b.handler.paginationManager.NavigateTo(event, s, b.page, "")
}

// handleBotSettingSelection processes select menu interactions.
func (b *BotMenu) handleBotSettingSelection(event *events.ComponentInteractionCreate, s *session.Session, _ string, option string) {
	// Show the change menu for the selected setting
	b.handler.settingMenu.ShowMenu(event, s, constants.BotSettingPrefix, option)
}

// handleBotSettingButton processes button interactions.
func (b *BotMenu) handleBotSettingButton(event *events.ComponentInteractionCreate, s *session.Session, customID string) {
	if customID == constants.BackButtonCustomID {
		b.handler.dashboardHandler.ShowDashboard(event, s, "")
	}
}

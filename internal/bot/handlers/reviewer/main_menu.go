package reviewer

import (
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/rotector/rotector/internal/bot/handlers/reviewer/builders"
	"github.com/rotector/rotector/internal/bot/handlers/reviewer/constants"
	"github.com/rotector/rotector/internal/bot/interfaces"
	"github.com/rotector/rotector/internal/bot/pagination"
	"github.com/rotector/rotector/internal/bot/session"
	"go.uber.org/zap"
)

// MainMenu handles the main menu functionality.
type MainMenu struct {
	handler *Handler
	page    *pagination.Page
}

// NewMainMenu creates a new MainMenu instance.
func NewMainMenu(h *Handler) *MainMenu {
	m := MainMenu{
		handler: h,
		page: &pagination.Page{
			Name: "Main Menu",
			Data: make(map[string]interface{}),
			Message: func(data map[string]interface{}) *discord.MessageUpdateBuilder {
				pendingCount := data["pendingCount"].(int)
				flaggedCount := data["flaggedCount"].(int)

				return builders.NewMainBuilder(pendingCount, flaggedCount).Build()
			},
			SelectHandlerFunc: func(event *events.ComponentInteractionCreate, s *session.Session, customID string, option string) {
				if customID == constants.ActionSelectMenuCustomID && option == constants.StartReviewCustomID {
					s.Set(session.KeySortBy, "random")
					h.reviewMenu.ShowReviewMenuAndFetchUser(event, s, "")
				}
			},
		},
	}
	return &m
}

// ShowMainMenu displays the main menu.
func (m *MainMenu) ShowMainMenu(event interfaces.CommonEvent) {
	s := m.handler.sessionManager.GetOrCreateSession(event.User().ID)

	// Get pending and flagged users count
	pendingCount, err := m.handler.db.Users().GetPendingUsersCount()
	if err != nil {
		m.handler.logger.Error("Failed to get pending users count", zap.Error(err))
	}

	flaggedCount, err := m.handler.db.Users().GetFlaggedUsersCount()
	if err != nil {
		m.handler.logger.Error("Failed to get flagged users count", zap.Error(err))
	}

	// Set data for the main menu
	m.page.Data["pendingCount"] = pendingCount
	m.page.Data["flaggedCount"] = flaggedCount

	// Navigate to the main menu and update the message
	m.handler.paginationManager.NavigateTo(m.page.Name, s)
	m.handler.paginationManager.UpdateMessage(event, s, m.page, "")
}

package dashboard

import (
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/rotector/rotector/internal/bot/constants"
	"github.com/rotector/rotector/internal/bot/handlers/dashboard/builders"
	"github.com/rotector/rotector/internal/bot/interfaces"
	"github.com/rotector/rotector/internal/bot/pagination"
	"github.com/rotector/rotector/internal/bot/session"
	"go.uber.org/zap"
)

// Dashboard represents the dashboard.
type Dashboard struct {
	handler *Handler
	page    *pagination.Page
}

// NewDashboard creates a new Dashboard instance.
func NewDashboard(h *Handler) *Dashboard {
	m := Dashboard{handler: h}
	m.page = &pagination.Page{
		Name: "Dashboard",
		Data: make(map[string]interface{}),
		Message: func(data map[string]interface{}) *discord.MessageUpdateBuilder {
			pendingCount := data["pendingCount"].(int)
			flaggedCount := data["flaggedCount"].(int)

			return builders.NewDashboardBuilder(pendingCount, flaggedCount).Build()
		},
		SelectHandlerFunc: m.handleSelectMenu,
	}
	return &m
}

// ShowDashboard displays the dashboard.
func (m *Dashboard) ShowDashboard(event interfaces.CommonEvent) {
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

// handleSelectMenu handles the select menu interaction.
func (m *Dashboard) handleSelectMenu(event *events.ComponentInteractionCreate, s *session.Session, customID string, option string) {
	if customID != constants.ActionSelectMenuCustomID {
		return
	}

	switch option {
	case constants.StartReviewCustomID:
		// Get user's default sort
		preferences, err := m.handler.db.Settings().GetUserPreferences(uint64(event.User().ID))
		if err != nil {
			m.handler.logger.Error("Failed to get user preferences", zap.Error(err))
		}
		s.Set(session.KeySortBy, preferences.DefaultSort)

		m.handler.reviewHandler.ShowReviewMenuAndFetchUser(event, s, "")
	case constants.UserPreferencesCustomID:
		m.handler.settingsHandler.ShowUserSettings(event, s)
	case constants.GuildSettingsCustomID:
		m.handler.settingsHandler.ShowGuildSettings(event, s)
	}
}

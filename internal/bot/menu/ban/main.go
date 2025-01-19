package ban

import (
	"context"
	"database/sql"
	"errors"

	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/builder/ban"
	"github.com/robalyx/rotector/internal/bot/core/pagination"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/interfaces"
	"go.uber.org/zap"
)

// Menu handles the display of ban information to banned users.
type Menu struct {
	layout *Layout
	page   *pagination.Page
}

// NewMenu creates a ban menu.
func NewMenu(layout *Layout) *Menu {
	m := &Menu{layout: layout}
	m.page = &pagination.Page{
		Name: "Ban Information",
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return ban.NewBuilder(s).Build()
		},
	}
	return m
}

// Show displays the ban information to the user.
func (m *Menu) Show(event interfaces.CommonEvent, s *session.Session) {
	// Get ban information
	ban, err := m.layout.db.Bans().GetBan(context.Background(), s.UserID())
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			m.layout.dashboardLayout.Show(event, s, "")
			return
		}

		m.layout.logger.Error("Failed to get ban information",
			zap.Error(err),
			zap.Uint64("user_id", s.UserID()))
		m.layout.paginationManager.RespondWithError(event, "Failed to retrieve ban information. Please try again later.")
		return
	}

	// If ban is expired, remove it and return to dashboard
	if ban.IsExpired() {
		if _, err := m.layout.db.Bans().UnbanUser(context.Background(), s.UserID()); err != nil {
			m.layout.logger.Error("Failed to remove expired ban",
				zap.Error(err),
				zap.Uint64("user_id", s.UserID()))
		}
		m.layout.dashboardLayout.Show(event, s, "Your ban has expired. You may now use the bot.")
		return
	}

	// Store ban in session and show the menu
	session.AdminBanInfo.Set(s, ban)
	m.layout.paginationManager.NavigateTo(event, s, m.page, "")
}

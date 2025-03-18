package ban

import (
	"context"
	"database/sql"
	"errors"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/robalyx/rotector/internal/bot/builder/ban"
	"github.com/robalyx/rotector/internal/bot/constants"
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
		Name: constants.BanPageName,
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return ban.NewBuilder(s).Build()
		},
		ShowHandlerFunc:   m.Show,
		ButtonHandlerFunc: m.handleButton,
	}
	return m
}

// Show displays the ban information to the user.
func (m *Menu) Show(event interfaces.CommonEvent, s *session.Session, r *pagination.Respond) {
	userID := session.UserID.Get(s)

	// Get ban information
	ban, err := m.layout.db.Model().Ban().GetBan(context.Background(), userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			r.Show(event, s, constants.DashboardPageName, "")
			return
		}

		m.layout.logger.Error("Failed to get ban information",
			zap.Error(err),
			zap.Uint64("user_id", userID))
		r.Error(event, "Failed to retrieve ban information. Please try again later.")
		return
	}

	// If ban is expired, remove it and return to dashboard
	if ban.IsExpired() {
		if _, err := m.layout.db.Model().Ban().UnbanUser(context.Background(), userID); err != nil {
			m.layout.logger.Error("Failed to remove expired ban",
				zap.Error(err),
				zap.Uint64("user_id", userID))
		}
		r.Show(event, s, constants.DashboardPageName, "Your ban has expired. You may now use the bot.")
		return
	}

	// Store ban in session and show the menu
	session.AdminBanInfo.Set(s, ban)
}

// handleButton processes button interactions.
func (m *Menu) handleButton(
	event *events.ComponentInteractionCreate, s *session.Session, r *pagination.Respond, customID string,
) {
	switch customID {
	case constants.AppealMenuButtonCustomID:
		r.Show(event, s, constants.AppealOverviewPageName, "")
	}
}

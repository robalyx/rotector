package ban

import (
	"database/sql"
	"errors"

	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/builder/ban"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/interaction"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"go.uber.org/zap"
)

// Menu handles the display of ban information to banned users.
type Menu struct {
	layout *Layout
	page   *interaction.Page
}

// NewMenu creates a ban menu.
func NewMenu(layout *Layout) *Menu {
	m := &Menu{layout: layout}
	m.page = &interaction.Page{
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
func (m *Menu) Show(ctx *interaction.Context, s *session.Session) {
	userID := session.UserID.Get(s)

	// Get ban information
	ban, err := m.layout.db.Model().Ban().GetBan(ctx.Context(), userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			ctx.Show(constants.DashboardPageName, "")
			return
		}

		m.layout.logger.Error("Failed to get ban information",
			zap.Error(err),
			zap.Uint64("user_id", userID))
		ctx.Error("Failed to retrieve ban information. Please try again later.")
		return
	}

	// If ban is expired, remove it and return to dashboard
	if ban.IsExpired() {
		if _, err := m.layout.db.Model().Ban().UnbanUser(ctx.Context(), userID); err != nil {
			m.layout.logger.Error("Failed to remove expired ban",
				zap.Error(err),
				zap.Uint64("user_id", userID))
		}
		ctx.Show(constants.DashboardPageName, "Your ban has expired. You may now use the bot.")
		return
	}

	// Store ban in session and show the menu
	session.AdminBanInfo.Set(s, ban)
}

// handleButton processes button interactions.
func (m *Menu) handleButton(ctx *interaction.Context, _ *session.Session, customID string) {
	switch customID {
	case constants.AppealMenuButtonCustomID:
		ctx.Show(constants.AppealOverviewPageName, "")
	}
}

package consent

import (
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/interaction"
	"github.com/robalyx/rotector/internal/bot/core/session"
	view "github.com/robalyx/rotector/internal/bot/views/consent"
	"github.com/robalyx/rotector/internal/database/types"
	"go.uber.org/zap"
)

// Menu handles the terms of service consent interface.
type Menu struct {
	layout *Layout
	page   *interaction.Page
}

// NewMenu creates a new consent menu.
func NewMenu(layout *Layout) *Menu {
	m := &Menu{layout: layout}
	m.page = &interaction.Page{
		Name: constants.ConsentPageName,
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return view.NewBuilder(s).Build()
		},
		ButtonHandlerFunc: m.handleButton,
	}

	return m
}

// handleButton processes button interactions.
func (m *Menu) handleButton(ctx *interaction.Context, _ *session.Session, customID string) {
	switch customID {
	case constants.ConsentAcceptButtonCustomID:
		m.handleAccept(ctx)
	case constants.ConsentRejectButtonCustomID:
		m.handleReject(ctx)
	case constants.AppealMenuButtonCustomID:
		ctx.Show(constants.AppealOverviewPageName, "")
	}
}

// handleAccept processes consent acceptance.
func (m *Menu) handleAccept(ctx *interaction.Context) {
	// Record consent
	consent := &types.UserConsent{
		DiscordUserID: uint64(ctx.Event().User().ID),
		ConsentedAt:   time.Now(),
		Version:       "1.0",
		AgeVerified:   false,
	}

	err := m.layout.db.Model().Consent().SaveConsent(ctx.Context(), consent)
	if err != nil {
		m.layout.logger.Error("Failed to save consent", zap.Error(err))
		ctx.Error("Failed to save consent. Please try again.")

		return
	}

	// Show dashboard
	ctx.Show(constants.DashboardPageName, "Welcome to Rotector!")
}

// handleReject processes consent rejection.
func (m *Menu) handleReject(ctx *interaction.Context) {
	ctx.Error("You must accept the terms of service to use Rotector. Please try again when ready.")
}

package consent

import (
	"context"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	builder "github.com/robalyx/rotector/internal/bot/builder/consent"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/pagination"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/interfaces"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"go.uber.org/zap"
)

// Menu handles the terms of service consent interface.
type Menu struct {
	layout *Layout
	page   *pagination.Page
}

// NewMenu creates a new consent menu.
func NewMenu(layout *Layout) *Menu {
	m := &Menu{layout: layout}
	m.page = &pagination.Page{
		Name: constants.ConsentPageName,
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return builder.NewBuilder(s).Build()
		},
		ShowHandlerFunc:   m.Show,
		ButtonHandlerFunc: m.handleButton,
	}
	return m
}

// Show prepares and displays the consent interface.
func (m *Menu) Show(_ interfaces.CommonEvent, _ *session.Session, _ *pagination.Respond) {
	// Do nothing
}

// handleButton processes button interactions.
func (m *Menu) handleButton(
	event *events.ComponentInteractionCreate, s *session.Session, r *pagination.Respond, customID string,
) {
	switch customID {
	case constants.ConsentAcceptButtonCustomID:
		m.handleAccept(event, s, r)
	case constants.ConsentRejectButtonCustomID:
		m.handleReject(event, r)
	case constants.AppealMenuButtonCustomID:
		r.Show(event, s, constants.AppealOverviewPageName, "")
	}
}

// handleAccept processes consent acceptance.
func (m *Menu) handleAccept(event *events.ComponentInteractionCreate, s *session.Session, r *pagination.Respond) {
	// Record consent
	consent := &types.UserConsent{
		DiscordUserID: uint64(event.User().ID),
		ConsentedAt:   time.Now(),
		Version:       "1.0",
		AgeVerified:   false,
	}

	err := m.layout.db.Model().Consent().SaveConsent(context.Background(), consent)
	if err != nil {
		m.layout.logger.Error("Failed to save consent", zap.Error(err))
		r.Error(event, "Failed to save consent. Please try again.")
		return
	}

	// Show dashboard
	r.Show(event, s, constants.DashboardPageName, "Welcome to Rotector!")
}

// handleReject processes consent rejection.
func (m *Menu) handleReject(event *events.ComponentInteractionCreate, r *pagination.Respond) {
	r.Error(event, "You must accept the terms of service to use Rotector. Please try again when ready.")
}

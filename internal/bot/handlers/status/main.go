package status

import (
	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/interaction"
	"github.com/robalyx/rotector/internal/bot/core/session"
	view "github.com/robalyx/rotector/internal/bot/views/status"
	"go.uber.org/zap"
)

// Menu handles worker status operations and their interactions.
type Menu struct {
	layout *Layout
	page   *interaction.Page
}

// NewMenu creates a new status menu.
func NewMenu(layout *Layout) *Menu {
	m := &Menu{layout: layout}
	m.page = &interaction.Page{
		Name: constants.StatusPageName,
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return view.NewBuilder(s).Build()
		},
		ShowHandlerFunc:   m.Show,
		ButtonHandlerFunc: m.handleButton,
	}

	return m
}

// Show prepares and displays the worker status interface.
func (m *Menu) Show(ctx *interaction.Context, s *session.Session) {
	// Get worker statuses
	workerStatuses, err := m.layout.workerMonitor.GetAllStatuses(ctx.Context())
	if err != nil {
		m.layout.logger.Error("Failed to get worker statuses", zap.Error(err))
	}

	// Store data in session
	session.StatusWorkers.Set(s, workerStatuses)
}

// handleButton processes button interactions, mainly handling refresh requests.
func (m *Menu) handleButton(ctx *interaction.Context, _ *session.Session, customID string) {
	switch customID {
	case constants.BackButtonCustomID:
		ctx.NavigateBack("")
	case constants.RefreshButtonCustomID:
		ctx.Reload("")
	}
}

package status

import (
	"context"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	builder "github.com/robalyx/rotector/internal/bot/builder/status"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/pagination"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/interfaces"
	"go.uber.org/zap"
)

// Menu handles worker status operations and their interactions.
type Menu struct {
	layout *Layout
	page   *pagination.Page
}

// NewMenu creates a new status menu.
func NewMenu(layout *Layout) *Menu {
	m := &Menu{layout: layout}
	m.page = &pagination.Page{
		Name: constants.StatusPageName,
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return builder.NewBuilder(s).Build()
		},
		ShowHandlerFunc:   m.Show,
		ButtonHandlerFunc: m.handleButton,
	}
	return m
}

// Show prepares and displays the worker status interface.
func (m *Menu) Show(_ interfaces.CommonEvent, s *session.Session, _ *pagination.Respond) {
	// Get worker statuses
	workerStatuses, err := m.layout.workerMonitor.GetAllStatuses(context.Background())
	if err != nil {
		m.layout.logger.Error("Failed to get worker statuses", zap.Error(err))
	}

	// Store data in session
	session.StatusWorkers.Set(s, workerStatuses)
}

// handleButton processes button interactions, mainly handling refresh requests.
func (m *Menu) handleButton(event *events.ComponentInteractionCreate, s *session.Session, r *pagination.Respond, customID string) {
	switch customID {
	case constants.BackButtonCustomID:
		r.NavigateBack(event, s, "")
	case constants.RefreshButtonCustomID:
		r.Reload(event, s, "")
	}
}

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

// MainMenu handles worker status operations and their interactions.
type MainMenu struct {
	layout *Layout
	page   *pagination.Page
}

// NewMainMenu creates a MainMenu by initializing the status menu and registering its
// page with the pagination manager.
func NewMainMenu(layout *Layout) *MainMenu {
	m := &MainMenu{layout: layout}
	m.page = &pagination.Page{
		Name: "Worker Status",
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return builder.NewBuilder(s).Build()
		},
		ButtonHandlerFunc: m.handleButton,
	}
	return m
}

// Show prepares and displays the worker status interface.
func (m *MainMenu) Show(event interfaces.CommonEvent, s *session.Session) {
	// Get worker statuses
	workerStatuses, err := m.layout.workerMonitor.GetAllStatuses(context.Background())
	if err != nil {
		m.layout.logger.Error("Failed to get worker statuses", zap.Error(err))
	}

	// Store data in session
	s.Set(constants.SessionKeyWorkerStatuses, workerStatuses)

	m.layout.paginationManager.NavigateTo(event, s, m.page, "")
}

// handleButton processes button interactions, mainly handling refresh requests.
func (m *MainMenu) handleButton(event *events.ComponentInteractionCreate, s *session.Session, customID string) {
	switch customID {
	case constants.BackButtonCustomID:
		m.layout.paginationManager.NavigateBack(event, s, "")
	case constants.RefreshButtonCustomID:
		m.Show(event, s)
	}
}

package ban

import (
	"github.com/robalyx/rotector/internal/bot/core/pagination"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/interfaces"
	"github.com/robalyx/rotector/internal/common/setup"
	"github.com/robalyx/rotector/internal/common/storage/database"
	"go.uber.org/zap"
)

// Layout handles the ban menu and its interactions.
type Layout struct {
	db                database.Client
	sessionManager    *session.Manager
	paginationManager *pagination.Manager
	logger            *zap.Logger
	menu              *Menu
	dashboardLayout   interfaces.DashboardLayout
}

// New creates a Layout by initializing the ban menu and registering its
// page with the pagination manager.
func New(
	app *setup.App,
	sessionManager *session.Manager,
	paginationManager *pagination.Manager,
	dashboardLayout interfaces.DashboardLayout,
) *Layout {
	// Initialize layout
	l := &Layout{
		db:                app.DB,
		sessionManager:    sessionManager,
		paginationManager: paginationManager,
		logger:            app.Logger,
		dashboardLayout:   dashboardLayout,
	}

	// Initialize menu with reference to this layout
	l.menu = NewMenu(l)

	// Register page with the pagination manager
	paginationManager.AddPage(l.menu.page)

	return l
}

// Show prepares and displays the ban interface.
func (l *Layout) Show(event interfaces.CommonEvent, s *session.Session) {
	l.menu.Show(event, s)
}

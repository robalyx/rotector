package appeal

import (
	"github.com/rotector/rotector/internal/bot/core/pagination"
	"github.com/rotector/rotector/internal/bot/core/session"
	"github.com/rotector/rotector/internal/bot/interfaces"
	"github.com/rotector/rotector/internal/common/setup"
	"github.com/rotector/rotector/internal/common/storage/database"
	"go.uber.org/zap"
)

// Layout handles the appeal menu and its dependencies.
type Layout struct {
	db                *database.Client
	logger            *zap.Logger
	sessionManager    *session.Manager
	paginationManager *pagination.Manager
	overviewMenu      *OverviewMenu
	ticketMenu        *TicketMenu
	dashboardLayout   interfaces.DashboardLayout
	userReviewLayout  interfaces.UserReviewLayout
}

// New creates a Layout by initializing the appeal menu and registering its
// page with the pagination manager.
func New(
	app *setup.App,
	sessionManager *session.Manager,
	paginationManager *pagination.Manager,
	dashboardLayout interfaces.DashboardLayout,
	userReviewLayout interfaces.UserReviewLayout,
) *Layout {
	// Initialize layout
	l := &Layout{
		db:                app.DB,
		logger:            app.Logger,
		sessionManager:    sessionManager,
		paginationManager: paginationManager,
		dashboardLayout:   dashboardLayout,
		userReviewLayout:  userReviewLayout,
	}

	// Initialize menu with reference to this layout
	l.overviewMenu = NewOverviewMenu(l)
	l.ticketMenu = NewTicketMenu(l)

	// Register menu page with the pagination manager
	paginationManager.AddPage(l.overviewMenu.page)
	paginationManager.AddPage(l.ticketMenu.page)

	return l
}

// ShowOverview displays the appeal overview menu.
func (l *Layout) ShowOverview(event interfaces.CommonEvent, s *session.Session, content string) {
	l.overviewMenu.Show(event, s, content)
}

// ShowTicket displays a specific appeal ticket.
func (l *Layout) ShowTicket(event interfaces.CommonEvent, s *session.Session, appealID int64, content string) {
	l.ticketMenu.Show(event, s, appealID, content)
}

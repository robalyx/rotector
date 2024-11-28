package queue

import (
	"github.com/rotector/rotector/internal/bot/core/pagination"
	"github.com/rotector/rotector/internal/bot/core/session"
	"github.com/rotector/rotector/internal/bot/interfaces"
	"github.com/rotector/rotector/internal/common/queue"
	"github.com/rotector/rotector/internal/common/storage/database"
	"go.uber.org/zap"
)

// Layout handles queue management operations and their interactions.
type Layout struct {
	db                *database.Client
	logger            *zap.Logger
	sessionManager    *session.Manager
	paginationManager *pagination.Manager
	queueManager      *queue.Manager
	mainMenu          *MainMenu
	dashboardLayout   interfaces.DashboardLayout
	userReviewLayout  interfaces.UserReviewLayout
}

// New creates a Layout by initializing the queue menu and registering its
// page with the pagination manager.
func New(
	db *database.Client,
	logger *zap.Logger,
	sessionManager *session.Manager,
	paginationManager *pagination.Manager,
	queueManager *queue.Manager,
	dashboardLayout interfaces.DashboardLayout,
	userReviewLayout interfaces.UserReviewLayout,
) *Layout {
	// Initialize layout
	l := &Layout{
		db:                db,
		logger:            logger,
		sessionManager:    sessionManager,
		paginationManager: paginationManager,
		queueManager:      queueManager,
		dashboardLayout:   dashboardLayout,
		userReviewLayout:  userReviewLayout,
	}
	l.mainMenu = NewMainMenu(l)

	// Initialize and register page
	paginationManager.AddPage(l.mainMenu.page)

	return l
}

// Show prepares and displays the queue interface by loading
// current queue lengths into the session.
func (l *Layout) Show(event interfaces.CommonEvent, s *session.Session) {
	l.mainMenu.Show(event, s, "")
}

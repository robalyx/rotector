package queue

import (
	"github.com/robalyx/rotector/internal/bot/core/pagination"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/interfaces"
	"github.com/robalyx/rotector/internal/common/queue"
	"github.com/robalyx/rotector/internal/common/setup"
	"github.com/robalyx/rotector/internal/common/storage/database"
	"go.uber.org/zap"
)

// Layout handles queue management operations and their interactions.
type Layout struct {
	db                database.Client
	logger            *zap.Logger
	sessionManager    *session.Manager
	paginationManager *pagination.Manager
	queueManager      *queue.Manager
	mainMenu          *MainMenu
	userReviewLayout  interfaces.UserReviewLayout
}

// New creates a Layout by initializing the queue menu and registering its
// page with the pagination manager.
func New(
	app *setup.App,
	sessionManager *session.Manager,
	paginationManager *pagination.Manager,
	userReviewLayout interfaces.UserReviewLayout,
) *Layout {
	// Initialize layout
	l := &Layout{
		db:                app.DB,
		logger:            app.Logger,
		sessionManager:    sessionManager,
		paginationManager: paginationManager,
		queueManager:      app.Queue,
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

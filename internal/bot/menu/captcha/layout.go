package captcha

import (
	"github.com/robalyx/rotector/internal/bot/core/pagination"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/interfaces"
	"github.com/robalyx/rotector/internal/common/setup"
	"github.com/robalyx/rotector/internal/common/storage/database"
	"go.uber.org/zap"
)

// Layout handles the CAPTCHA verification interface.
type Layout struct {
	db                database.Client
	sessionManager    *session.Manager
	paginationManager *pagination.Manager
	menu              *Menu
	logger            *zap.Logger
}

// New creates a new Layout by initializing the CAPTCHA menu and registering
// its page with the pagination manager.
func New(
	app *setup.App,
	sessionManager *session.Manager,
	paginationManager *pagination.Manager,
) *Layout {
	// Initialize layout
	l := &Layout{
		db:                app.DB,
		sessionManager:    sessionManager,
		paginationManager: paginationManager,
		logger:            app.Logger,
	}

	// Initialize menu with reference to this layout
	l.menu = NewMenu(l)

	// Register menu page with the pagination manager
	paginationManager.AddPage(l.menu.page)

	return l
}

// Show prepares and displays the CAPTCHA verification interface.
func (l *Layout) Show(event interfaces.CommonEvent, s *session.Session, content string) {
	l.menu.Show(event, s, content)
}

package ban

import (
	"github.com/robalyx/rotector/internal/bot/core/pagination"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/common/setup"
	"github.com/robalyx/rotector/internal/common/storage/database"
	"go.uber.org/zap"
)

// Layout handles the ban menu and its interactions.
type Layout struct {
	db             database.Client
	sessionManager *session.Manager
	logger         *zap.Logger
	menu           *Menu
}

// New creates a Layout by initializing the ban menu.
func New(app *setup.App, sessionManager *session.Manager) *Layout {
	// Initialize layout
	l := &Layout{
		db:             app.DB,
		sessionManager: sessionManager,
		logger:         app.Logger,
	}

	// Initialize menu with reference to this layout
	l.menu = NewMenu(l)

	return l
}

// Pages returns all the pages in the layout.
func (l *Layout) Pages() []*pagination.Page {
	return []*pagination.Page{
		l.menu.page,
	}
}

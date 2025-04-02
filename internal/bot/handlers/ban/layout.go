package ban

import (
	"github.com/robalyx/rotector/internal/bot/core/interaction"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/database"
	"github.com/robalyx/rotector/internal/setup"
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
		logger:         app.Logger.Named("ban_menu"),
	}

	// Initialize menu with reference to this layout
	l.menu = NewMenu(l)

	return l
}

// Pages returns all the pages in the layout.
func (l *Layout) Pages() []*interaction.Page {
	return []*interaction.Page{
		l.menu.page,
	}
}

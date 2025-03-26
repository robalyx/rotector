package selector

import (
	"github.com/robalyx/rotector/internal/bot/core/interaction"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/common/setup"
	"go.uber.org/zap"
)

// Layout handles the display and interaction logic for the selector menu.
type Layout struct {
	sessionManager *session.Manager
	menu           *Menu
	logger         *zap.Logger
}

// New creates a Layout by initializing the selector menu.
func New(app *setup.App, sessionManager *session.Manager) *Layout {
	l := &Layout{
		sessionManager: sessionManager,
		logger:         app.Logger.Named("selector_menu"),
	}
	l.menu = NewMenu(l)
	return l
}

// Pages returns all the pages in the layout.

func (l *Layout) Pages() []*interaction.Page {
	return []*interaction.Page{
		l.menu.page,
	}
}

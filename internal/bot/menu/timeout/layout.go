package timeout

import (
	"github.com/robalyx/rotector/internal/bot/core/pagination"
	"github.com/robalyx/rotector/internal/common/setup"
	"github.com/robalyx/rotector/internal/common/storage/database"
	"go.uber.org/zap"
)

// Layout handles the timeout menu interface.
type Layout struct {
	db     database.Client
	menu   *Menu
	logger *zap.Logger
}

// New creates a Layout by initializing the timeout menu.
func New(app *setup.App) *Layout {
	l := &Layout{
		db:     app.DB,
		logger: app.Logger,
	}
	l.menu = NewMenu(l)
	return l
}

// Pages returns all the pages in the layout.
func (l *Layout) Pages() []*pagination.Page {
	return []*pagination.Page{
		l.menu.page,
	}
}

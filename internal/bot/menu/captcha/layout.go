package captcha

import (
	"github.com/robalyx/rotector/internal/bot/core/pagination"
	"github.com/robalyx/rotector/internal/common/setup"
	"github.com/robalyx/rotector/internal/common/storage/database"
	"go.uber.org/zap"
)

// Layout handles the CAPTCHA verification interface.
type Layout struct {
	db     database.Client
	menu   *Menu
	logger *zap.Logger
}

// New creates a new Layout by initializing the CAPTCHA menu.
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

package consent

import (
	"github.com/robalyx/rotector/internal/bot/core/interaction"
	"github.com/robalyx/rotector/internal/common/setup"
	"github.com/robalyx/rotector/internal/common/storage/database"
	"go.uber.org/zap"
)

// Layout handles the consent menu operations.
type Layout struct {
	db     database.Client
	menu   *Menu
	logger *zap.Logger
}

// New creates a Layout by initializing the consent menu.
func New(app *setup.App) *Layout {
	l := &Layout{
		db:     app.DB,
		logger: app.Logger.Named("consent_menu"),
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

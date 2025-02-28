package guild

import (
	"github.com/robalyx/rotector/internal/bot/core/pagination"
	"github.com/robalyx/rotector/internal/common/setup"
	"github.com/robalyx/rotector/internal/common/storage/database"
	"go.uber.org/zap"
)

// Layout handles the display and interaction logic for the guild menu.
type Layout struct {
	db     database.Client
	menu   *Menu
	scan   *ScanMenu
	logs   *LogsMenu
	lookup *LookupMenu
	logger *zap.Logger
}

// New creates a Layout by initializing the guild menu.
func New(app *setup.App) *Layout {
	l := &Layout{
		db:     app.DB,
		logger: app.Logger,
	}

	l.menu = NewMenu(l)
	l.scan = NewScanMenu(l)
	l.logs = NewLogsMenu(l)
	l.lookup = NewLookupMenu(l)

	return l
}

// Pages returns all the pages in the layout.
func (l *Layout) Pages() []*pagination.Page {
	return []*pagination.Page{
		l.menu.page,
		l.scan.page,
		l.logs.page,
		l.lookup.page,
	}
}

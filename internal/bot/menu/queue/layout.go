package queue

import (
	"github.com/robalyx/rotector/internal/bot/core/interaction"
	"github.com/robalyx/rotector/internal/common/queue"
	"github.com/robalyx/rotector/internal/common/setup"
	"github.com/robalyx/rotector/internal/common/storage/database"
	"go.uber.org/zap"
)

// Layout handles queue management operations and their interactions.
type Layout struct {
	db           database.Client
	logger       *zap.Logger
	queueManager *queue.Manager
	menu         *Menu
}

// New creates a Layout by initializing the queue menu.
func New(app *setup.App) *Layout {
	l := &Layout{
		db:           app.DB,
		logger:       app.Logger.Named("queue_menu"),
		queueManager: app.Queue,
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

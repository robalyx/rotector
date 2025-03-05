package chat

import (
	"github.com/robalyx/rotector/internal/bot/core/pagination"
	"github.com/robalyx/rotector/internal/common/client/ai"
	"github.com/robalyx/rotector/internal/common/setup"
	"github.com/robalyx/rotector/internal/common/storage/database"
	"go.uber.org/zap"
)

// Layout handles the chat interface and AI interactions.
type Layout struct {
	db          database.Client
	chatHandler *ai.ChatHandler
	menu        *Menu
	logger      *zap.Logger
}

// New creates a layout by initializing the chat menu.
func New(app *setup.App) *Layout {
	l := &Layout{
		db:          app.DB,
		chatHandler: ai.NewChatHandler(app.GenAIClient, app.Logger),
		logger:      app.Logger.Named("chat_menu"),
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

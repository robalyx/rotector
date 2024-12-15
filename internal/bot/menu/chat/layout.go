package chat

import (
	"github.com/rotector/rotector/internal/bot/core/pagination"
	"github.com/rotector/rotector/internal/bot/core/session"
	"github.com/rotector/rotector/internal/bot/interfaces"
	"github.com/rotector/rotector/internal/common/client/ai"
	"github.com/rotector/rotector/internal/common/setup"
	"github.com/rotector/rotector/internal/common/storage/database"
	"go.uber.org/zap"
)

// Layout handles the chat interface and AI interactions.
type Layout struct {
	db                *database.Client
	sessionManager    *session.Manager
	paginationManager *pagination.Manager
	chatHandler       *ai.ChatHandler
	menu              *Menu
	dashboardLayout   interfaces.DashboardLayout
	logger            *zap.Logger
}

// New creates a new chat layout.
func New(
	app *setup.App,
	sessionManager *session.Manager,
	paginationManager *pagination.Manager,
	dashboardLayout interfaces.DashboardLayout,
) *Layout {
	l := &Layout{
		db:                app.DB,
		sessionManager:    sessionManager,
		paginationManager: paginationManager,
		chatHandler:       ai.NewChatHandler(app.GenAIClient, app.Logger),
		dashboardLayout:   dashboardLayout,
		logger:            app.Logger,
	}

	// Initialize menu
	l.menu = NewMenu(l)

	// Register menu page
	paginationManager.AddPage(l.menu.page)

	return l
}

// Show displays the chat interface.
func (l *Layout) Show(event interfaces.CommonEvent, s *session.Session) {
	l.menu.Show(event, s, "")
}

package setting

import (
	"github.com/rotector/rotector/internal/bot/builder/setting"
	"github.com/rotector/rotector/internal/bot/core/pagination"
	"github.com/rotector/rotector/internal/bot/core/session"
	"github.com/rotector/rotector/internal/bot/interfaces"
	"github.com/rotector/rotector/internal/common/setup"
	"github.com/rotector/rotector/internal/common/storage/database"
	"go.uber.org/zap"
)

// Layout handles all setting-related menus and their interactions.
type Layout struct {
	db                *database.Client
	sessionManager    *session.Manager
	paginationManager *pagination.Manager
	updateMenu        *UpdateMenu
	userMenu          *UserMenu
	botMenu           *BotMenu
	registry          *setting.Registry
	logger            *zap.Logger
}

// New creates a Layout by initializing all setting menus and registering their
// pages with the pagination manager.
func New(
	app *setup.App,
	sessionManager *session.Manager,
	paginationManager *pagination.Manager,
) *Layout {
	// Initialize layout
	l := &Layout{
		db:                app.DB,
		logger:            app.Logger,
		sessionManager:    sessionManager,
		paginationManager: paginationManager,
		registry:          setting.NewRegistry(),
	}

	// Initialize all menus with references to this layout
	l.updateMenu = NewUpdateMenu(l)
	l.userMenu = NewUserMenu(l)
	l.botMenu = NewBotMenu(l)

	// Register menu pages with the pagination manager
	paginationManager.AddPage(l.userMenu.page)
	paginationManager.AddPage(l.botMenu.page)
	paginationManager.AddPage(l.updateMenu.page)

	return l
}

// ShowUser loads user settings from the database into the session and
// displays them through the pagination system.
func (l *Layout) ShowUser(event interfaces.CommonEvent, s *session.Session) {
	l.userMenu.Show(event, s)
}

// ShowBot loads bot settings into the session, then displays them through the pagination system.
func (l *Layout) ShowBot(event interfaces.CommonEvent, s *session.Session) {
	l.botMenu.Show(event, s)
}

// ShowUpdate loads the update menu and displays it through the pagination system.
func (l *Layout) ShowUpdate(event interfaces.CommonEvent, s *session.Session, prefix string, option string) {
	l.updateMenu.Show(event, s, prefix, option)
}

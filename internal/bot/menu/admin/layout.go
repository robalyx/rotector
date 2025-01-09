package admin

import (
	"github.com/robalyx/rotector/internal/bot/core/pagination"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/interfaces"
	"github.com/robalyx/rotector/internal/common/setup"
	"github.com/robalyx/rotector/internal/common/storage/database"
	"go.uber.org/zap"
)

// Layout handles the admin menu and its submenus.
type Layout struct {
	db                *database.Client
	sessionManager    *session.Manager
	paginationManager *pagination.Manager
	logger            *zap.Logger
	mainMenu          *MainMenu
	confirmMenu       *ConfirmMenu
	settingLayout     interfaces.SettingLayout
}

// New creates a Layout by initializing all admin menus and registering their
// pages with the pagination manager.
func New(
	app *setup.App,
	sessionManager *session.Manager,
	paginationManager *pagination.Manager,
	settingLayout interfaces.SettingLayout,
) *Layout {
	// Initialize layout
	l := &Layout{
		db:                app.DB,
		sessionManager:    sessionManager,
		paginationManager: paginationManager,
		logger:            app.Logger,
		settingLayout:     settingLayout,
	}

	// Initialize menus with reference to this layout
	l.mainMenu = NewMainMenu(l)
	l.confirmMenu = NewConfirmMenu(l)

	// Register pages with the pagination manager
	paginationManager.AddPage(l.mainMenu.page)
	paginationManager.AddPage(l.confirmMenu.page)

	return l
}

// Show prepares and displays the admin interface.
func (l *Layout) Show(event interfaces.CommonEvent, s *session.Session) {
	l.mainMenu.Show(event, s, "")
}

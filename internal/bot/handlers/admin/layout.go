package admin

import (
	"github.com/robalyx/rotector/internal/bot/core/interaction"
	"github.com/robalyx/rotector/internal/cloudflare"
	"github.com/robalyx/rotector/internal/database"
	"github.com/robalyx/rotector/internal/setup"
	"go.uber.org/zap"
)

// Layout handles the admin menu and its submenus.
type Layout struct {
	db          database.Client
	cfClient    *cloudflare.Client
	logger      *zap.Logger
	mainMenu    *MainMenu
	confirmMenu *ConfirmMenu
}

// New creates a Layout by initializing all admin menus and registering their
// pages with the pagination manager.
func New(app *setup.App) *Layout {
	// Initialize layout
	l := &Layout{
		db:       app.DB,
		cfClient: app.CFClient,
		logger:   app.Logger.Named("admin_menu"),
	}

	// Initialize menus with reference to this layout
	l.mainMenu = NewMainMenu(l)
	l.confirmMenu = NewConfirmMenu(l)

	return l
}

// Pages returns all the pages in the layout.
func (l *Layout) Pages() []*interaction.Page {
	return []*interaction.Page{
		l.mainMenu.page,
		l.confirmMenu.page,
	}
}

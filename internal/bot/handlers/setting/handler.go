package setting

import (
	"github.com/rotector/rotector/internal/bot/interfaces"
	"github.com/rotector/rotector/internal/bot/pagination"
	"github.com/rotector/rotector/internal/bot/session"
	"github.com/rotector/rotector/internal/common/database"
	"go.uber.org/zap"
)

// Handler manages all setting-related menus and their interactions.
// It maintains references to the database, session manager, and other handlers
// needed for navigation between different parts of the settings menu.
type Handler struct {
	db                *database.Database
	sessionManager    *session.Manager
	paginationManager *pagination.Manager
	settingMenu       *Menu
	userMenu          *UserMenu
	guildMenu         *GuildMenu
	logger            *zap.Logger
	dashboardHandler  interfaces.DashboardHandler
}

// New creates a Handler by initializing all setting menus and registering their
// pages with the pagination manager.
func New(
	db *database.Database,
	logger *zap.Logger,
	sessionManager *session.Manager,
	paginationManager *pagination.Manager,
	dashboardHandler interfaces.DashboardHandler,
) *Handler {
	h := &Handler{
		db:                db,
		logger:            logger,
		sessionManager:    sessionManager,
		paginationManager: paginationManager,
		dashboardHandler:  dashboardHandler,
	}

	// Initialize all menus with references to this handler
	h.settingMenu = NewMenu(h)
	h.userMenu = NewUserMenu(h)
	h.guildMenu = NewGuildMenu(h)

	// Register menu pages with the pagination manager
	paginationManager.AddPage(h.userMenu.page)
	paginationManager.AddPage(h.guildMenu.page)
	paginationManager.AddPage(h.settingMenu.page)

	return h
}

// ShowUserSettings loads user settings from the database into the session and
// displays them through the pagination system.
func (h *Handler) ShowUserSettings(event interfaces.CommonEvent, s *session.Session) {
	h.userMenu.ShowMenu(event, s)
}

// ShowGuildSettings loads guild settings and available roles into the session,
// then displays them through the pagination system.
func (h *Handler) ShowGuildSettings(event interfaces.CommonEvent, s *session.Session) {
	h.guildMenu.ShowMenu(event, s)
}

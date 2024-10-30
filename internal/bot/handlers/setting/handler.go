package setting

import (
	"github.com/rotector/rotector/internal/bot/interfaces"
	"github.com/rotector/rotector/internal/bot/pagination"
	"github.com/rotector/rotector/internal/bot/session"
	"github.com/rotector/rotector/internal/common/database"
	"go.uber.org/zap"
)

// Handler manages the settings menu.
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

// New creates a new Handler instance.
func New(db *database.Database, logger *zap.Logger, sessionManager *session.Manager, paginationManager *pagination.Manager, dashboardHandler interfaces.DashboardHandler) *Handler {
	h := &Handler{
		db:                db,
		logger:            logger,
		sessionManager:    sessionManager,
		paginationManager: paginationManager,
		dashboardHandler:  dashboardHandler,
	}

	h.settingMenu = NewMenu(h)
	h.userMenu = NewUserMenu(h)
	h.guildMenu = NewGuildMenu(h)

	paginationManager.AddPage(h.userMenu.page)
	paginationManager.AddPage(h.guildMenu.page)
	paginationManager.AddPage(h.settingMenu.page)

	return h
}

// ShowUserSettings displays the user settings menu.
func (h *Handler) ShowUserSettings(event interfaces.CommonEvent, s *session.Session) {
	h.userMenu.ShowMenu(event, s)
}

// ShowGuildSettings displays the guild settings menu.
func (h *Handler) ShowGuildSettings(event interfaces.CommonEvent, s *session.Session) {
	h.guildMenu.ShowMenu(event, s)
}

package log

import (
	"github.com/rotector/rotector/internal/bot/interfaces"
	"github.com/rotector/rotector/internal/bot/pagination"
	"github.com/rotector/rotector/internal/bot/session"
	"github.com/rotector/rotector/internal/common/database"
	"go.uber.org/zap"
)

// Handler manages the log querying process.
type Handler struct {
	db                *database.Database
	sessionManager    *session.Manager
	paginationManager *pagination.Manager
	logMenu           *Menu
	logger            *zap.Logger
	dashboardHandler  interfaces.DashboardHandler
}

// New creates a new Handler instance.
func New(
	db *database.Database,
	sessionManager *session.Manager,
	paginationManager *pagination.Manager,
	dashboardHandler interfaces.DashboardHandler,
	logger *zap.Logger,
) *Handler {
	h := &Handler{
		db:                db,
		sessionManager:    sessionManager,
		paginationManager: paginationManager,
		dashboardHandler:  dashboardHandler,
		logger:            logger,
	}

	h.logMenu = NewMenu(h)
	paginationManager.AddPage(h.logMenu.page)

	return h
}

// ShowLogMenu displays the log querying menu.
func (h *Handler) ShowLogMenu(event interfaces.CommonEvent, s *session.Session) {
	h.logMenu.ShowLogMenu(event, s)
}

package log

import (
	"github.com/rotector/rotector/internal/bot/interfaces"
	"github.com/rotector/rotector/internal/bot/pagination"
	"github.com/rotector/rotector/internal/bot/session"
	"github.com/rotector/rotector/internal/common/database"
	"go.uber.org/zap"
)

// Handler coordinates log viewing operations and their interactions.
// It maintains references to the database, session manager, and other handlers
// needed for processing log queries.
type Handler struct {
	db                *database.Database
	sessionManager    *session.Manager
	paginationManager *pagination.Manager
	logMenu           *Menu
	logger            *zap.Logger
	dashboardHandler  interfaces.DashboardHandler
}

// New creates a Handler by initializing the log menu and registering its
// page with the pagination manager.
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

	// Initialize menu and register its page
	h.logMenu = NewMenu(h)
	paginationManager.AddPage(h.logMenu.page)

	return h
}

// ShowLogMenu prepares and displays the log interface by initializing
// session data with default values and loading user preferences.
func (h *Handler) ShowLogMenu(event interfaces.CommonEvent, s *session.Session) {
	h.logMenu.ShowLogMenu(event, s)
}

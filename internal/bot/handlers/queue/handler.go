package queue

import (
	"github.com/rotector/rotector/internal/bot/interfaces"
	"github.com/rotector/rotector/internal/bot/pagination"
	"github.com/rotector/rotector/internal/bot/session"
	"github.com/rotector/rotector/internal/common/database"
	"github.com/rotector/rotector/internal/common/queue"
	"go.uber.org/zap"
)

// Handler handles the queue functionality.
type Handler struct {
	db                *database.Database
	logger            *zap.Logger
	sessionManager    *session.Manager
	paginationManager *pagination.Manager
	queueManager      *queue.Manager
	queueMenu         *Menu
	dashboardHandler  interfaces.DashboardHandler
	reviewHandler     interfaces.ReviewHandler
}

// New creates a new Handler instance.
func New(
	db *database.Database,
	logger *zap.Logger,
	sessionManager *session.Manager,
	paginationManager *pagination.Manager,
	queueManager *queue.Manager,
	dashboardHandler interfaces.DashboardHandler,
	reviewHandler interfaces.ReviewHandler,
) *Handler {
	h := &Handler{
		db:                db,
		logger:            logger,
		sessionManager:    sessionManager,
		paginationManager: paginationManager,
		queueManager:      queueManager,
		dashboardHandler:  dashboardHandler,
		reviewHandler:     reviewHandler,
	}

	h.queueMenu = NewMenu(h)
	paginationManager.AddPage(h.queueMenu.page)

	return h
}

// ShowQueueMenu shows the queue menu.
func (h *Handler) ShowQueueMenu(event interfaces.CommonEvent, s *session.Session) {
	h.queueMenu.ShowQueueMenu(event, s)
}

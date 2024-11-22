package queue

import (
	"github.com/rotector/rotector/internal/bot/core/pagination"
	"github.com/rotector/rotector/internal/bot/core/session"
	"github.com/rotector/rotector/internal/bot/interfaces"
	"github.com/rotector/rotector/internal/common/queue"
	"github.com/rotector/rotector/internal/common/storage/database"
	"go.uber.org/zap"
)

// Handler coordinates queue management operations and their interactions.
// It maintains references to the database, queue manager, and other handlers
// needed for processing queue operations.
type Handler struct {
	db                *database.Client
	logger            *zap.Logger
	sessionManager    *session.Manager
	paginationManager *pagination.Manager
	queueManager      *queue.Manager
	queueMenu         *Menu
	dashboardHandler  interfaces.DashboardHandler
	userReviewHandler interfaces.UserReviewHandler
}

// New creates a Handler by initializing the queue menu and registering its
// page with the pagination manager.
func New(
	db *database.Client,
	logger *zap.Logger,
	sessionManager *session.Manager,
	paginationManager *pagination.Manager,
	queueManager *queue.Manager,
	dashboardHandler interfaces.DashboardHandler,
	userReviewHandler interfaces.UserReviewHandler,
) *Handler {
	h := &Handler{
		db:                db,
		logger:            logger,
		sessionManager:    sessionManager,
		paginationManager: paginationManager,
		queueManager:      queueManager,
		dashboardHandler:  dashboardHandler,
		userReviewHandler: userReviewHandler,
	}

	// Initialize menu and register its page
	h.queueMenu = NewMenu(h)
	paginationManager.AddPage(h.queueMenu.page)

	return h
}

// ShowQueueMenu prepares and displays the queue interface by loading
// current queue lengths into the session.
func (h *Handler) ShowQueueMenu(event interfaces.CommonEvent, s *session.Session) {
	h.queueMenu.ShowQueueMenu(event, s, "")
}

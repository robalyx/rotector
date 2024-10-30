package dashboard

import (
	"github.com/rotector/rotector/internal/bot/interfaces"
	"github.com/rotector/rotector/internal/bot/pagination"
	"github.com/rotector/rotector/internal/bot/session"
	"github.com/rotector/rotector/internal/common/database"
	"go.uber.org/zap"
)

// Handler handles the dashboard functionality.
type Handler struct {
	db                *database.Database
	logger            *zap.Logger
	sessionManager    *session.Manager
	paginationManager *pagination.Manager
	reviewHandler     interfaces.ReviewHandler
	settingsHandler   interfaces.SettingsHandler
	logsHandler       interfaces.LogsHandler
	queueHandler      interfaces.QueueHandler
	dashboard         *Dashboard
}

// New creates a new Handler instance.
func New(db *database.Database, logger *zap.Logger, sessionManager *session.Manager, paginationManager *pagination.Manager) *Handler {
	h := &Handler{
		db:                db,
		logger:            logger,
		sessionManager:    sessionManager,
		paginationManager: paginationManager,
	}

	// Add necessary menus
	h.dashboard = NewDashboard(h)

	// Add pages to the pagination manager
	paginationManager.AddPage(h.dashboard.page)

	return h
}

// SetReviewHandler sets the review handler.
func (h *Handler) SetReviewHandler(reviewHandler interfaces.ReviewHandler) {
	h.reviewHandler = reviewHandler
}

// SetSettingsHandler sets the settings handler.
func (h *Handler) SetSettingsHandler(settingsHandler interfaces.SettingsHandler) {
	h.settingsHandler = settingsHandler
}

// SetLogsHandler sets the logs handler.
func (h *Handler) SetLogsHandler(logsHandler interfaces.LogsHandler) {
	h.logsHandler = logsHandler
}

// SetQueueHandler sets the queue handler.
func (h *Handler) SetQueueHandler(queueHandler interfaces.QueueHandler) {
	h.queueHandler = queueHandler
}

// ShowDashboard shows the dashboard.
func (h *Handler) ShowDashboard(event interfaces.CommonEvent) {
	h.dashboard.ShowDashboard(event)
}

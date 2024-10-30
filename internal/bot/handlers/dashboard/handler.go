package dashboard

import (
	"github.com/rotector/rotector/internal/bot/interfaces"
	"github.com/rotector/rotector/internal/bot/pagination"
	"github.com/rotector/rotector/internal/bot/session"
	"github.com/rotector/rotector/internal/common/database"
	"github.com/rotector/rotector/internal/common/statistics"
	"go.uber.org/zap"
)

// Handler handles the dashboard functionality.
type Handler struct {
	db                *database.Database
	stats             *statistics.Statistics
	sessionManager    *session.Manager
	paginationManager *pagination.Manager
	dashboard         *Menu
	logger            *zap.Logger
	reviewHandler     interfaces.ReviewHandler
	settingsHandler   interfaces.SettingsHandler
	logsHandler       interfaces.LogsHandler
	queueHandler      interfaces.QueueHandler
}

// New creates a new Handler instance.
func New(db *database.Database, stats *statistics.Statistics, logger *zap.Logger, sessionManager *session.Manager, paginationManager *pagination.Manager) *Handler {
	h := &Handler{
		db:                db,
		stats:             stats,
		logger:            logger,
		sessionManager:    sessionManager,
		paginationManager: paginationManager,
	}

	h.dashboard = NewMenu(h)
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
func (h *Handler) ShowDashboard(event interfaces.CommonEvent, s *session.Session) {
	h.dashboard.ShowDashboard(event, s)
}

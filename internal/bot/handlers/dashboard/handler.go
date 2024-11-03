package dashboard

import (
	"github.com/rotector/rotector/internal/bot/interfaces"
	"github.com/rotector/rotector/internal/bot/pagination"
	"github.com/rotector/rotector/internal/bot/session"
	"github.com/rotector/rotector/internal/common/database"
	"github.com/rotector/rotector/internal/common/statistics"
	"go.uber.org/zap"
)

// Handler coordinates dashboard operations and their interactions.
// It maintains references to the database, statistics, and other handlers
// needed for navigation between different sections of the bot.
type Handler struct {
	db                *database.Database
	stats             *statistics.Client
	sessionManager    *session.Manager
	paginationManager *pagination.Manager
	dashboard         *Menu
	logger            *zap.Logger
	reviewHandler     interfaces.ReviewHandler
	settingsHandler   interfaces.SettingsHandler
	logsHandler       interfaces.LogsHandler
	queueHandler      interfaces.QueueHandler
}

// New creates a Handler by initializing the dashboard menu and registering its
// page with the pagination manager.
func New(
	db *database.Database,
	stats *statistics.Client,
	logger *zap.Logger,
	sessionManager *session.Manager,
	paginationManager *pagination.Manager,
) *Handler {
	h := &Handler{
		db:                db,
		stats:             stats,
		logger:            logger,
		sessionManager:    sessionManager,
		paginationManager: paginationManager,
	}

	// Initialize menu and register its page
	h.dashboard = NewMenu(h)
	paginationManager.AddPage(h.dashboard.page)

	return h
}

// SetReviewHandler links the review handler to enable navigation
// to the review section from the dashboard.
func (h *Handler) SetReviewHandler(reviewHandler interfaces.ReviewHandler) {
	h.reviewHandler = reviewHandler
}

// SetSettingsHandler links the settings handler to enable navigation
// to the settings section from the dashboard.
func (h *Handler) SetSettingsHandler(settingsHandler interfaces.SettingsHandler) {
	h.settingsHandler = settingsHandler
}

// SetLogsHandler links the logs handler to enable navigation
// to the logs section from the dashboard.
func (h *Handler) SetLogsHandler(logsHandler interfaces.LogsHandler) {
	h.logsHandler = logsHandler
}

// SetQueueHandler links the queue handler to enable navigation
// to the queue section from the dashboard.
func (h *Handler) SetQueueHandler(queueHandler interfaces.QueueHandler) {
	h.queueHandler = queueHandler
}

// ShowDashboard prepares and displays the dashboard interface by loading
// statistics and active user information into the session.
func (h *Handler) ShowDashboard(event interfaces.CommonEvent, s *session.Session, content string) {
	h.dashboard.ShowDashboard(event, s, content)
}

package dashboard

import (
	"github.com/rotector/rotector/internal/bot/interfaces"
	"github.com/rotector/rotector/internal/bot/pagination"
	"github.com/rotector/rotector/internal/bot/session"
	"github.com/rotector/rotector/internal/common/database"
	"github.com/rotector/rotector/internal/common/redis"
	"github.com/rotector/rotector/internal/common/worker"
	"go.uber.org/zap"
)

// Handler coordinates dashboard operations and their interactions.
// It maintains references to the database, statistics, and other handlers
// needed for navigation between different sections of the bot.
type Handler struct {
	db                *database.Database
	sessionManager    *session.Manager
	paginationManager *pagination.Manager
	workerMonitor     *worker.Monitor
	dashboard         *Menu
	logger            *zap.Logger
	reviewHandler     interfaces.ReviewHandler
	settingsHandler   interfaces.SettingsHandler
	logHandler        interfaces.LogHandler
	queueHandler      interfaces.QueueHandler
}

// New creates a Handler by initializing the dashboard menu and registering its
// page with the pagination manager.
func New(
	db *database.Database,
	logger *zap.Logger,
	sessionManager *session.Manager,
	paginationManager *pagination.Manager,
	redisManager *redis.Manager,
) *Handler {
	// Get Redis client for worker status
	statusClient, err := redisManager.GetClient(redis.WorkerStatusDBIndex)
	if err != nil {
		logger.Fatal("Failed to get Redis client for worker status", zap.Error(err))
	}

	h := &Handler{
		db:                db,
		logger:            logger,
		sessionManager:    sessionManager,
		paginationManager: paginationManager,
		workerMonitor:     worker.NewMonitor(statusClient, logger),
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

// SetLogHandler links the log handler to enable navigation
// to the logs section from the dashboard.
func (h *Handler) SetLogHandler(logHandler interfaces.LogHandler) {
	h.logHandler = logHandler
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

package dashboard

import (
	"github.com/rotector/rotector/internal/bot/core/pagination"
	"github.com/rotector/rotector/internal/bot/core/session"
	"github.com/rotector/rotector/internal/bot/interfaces"
	"github.com/rotector/rotector/internal/common/storage/database"
	"github.com/rotector/rotector/internal/common/storage/redis"
	"github.com/rotector/rotector/internal/worker/core"
	"go.uber.org/zap"
)

// Handler coordinates dashboard operations and their interactions.
// It maintains references to the database, statistics, and other handlers
// needed for navigation between different sections of the bot.
type Handler struct {
	db                 *database.Client
	sessionManager     *session.Manager
	paginationManager  *pagination.Manager
	workerMonitor      *core.Monitor
	dashboard          *Menu
	logger             *zap.Logger
	userReviewHandler  interfaces.UserReviewHandler
	groupReviewHandler interfaces.GroupReviewHandler
	settingsHandler    interfaces.SettingsHandler
	logHandler         interfaces.LogHandler
	queueHandler       interfaces.QueueHandler
}

// New creates a Handler by initializing the dashboard menu and registering its
// page with the pagination manager.
func New(
	db *database.Client,
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
		workerMonitor:     core.NewMonitor(statusClient, logger),
	}

	// Initialize menu and register its page
	h.dashboard = NewMenu(h)
	paginationManager.AddPage(h.dashboard.page)

	return h
}

// SetUserReviewHandler links the user review handler to enable navigation
// to the user review section from the dashboard.
func (h *Handler) SetUserReviewHandler(reviewHandler interfaces.UserReviewHandler) {
	h.userReviewHandler = reviewHandler
}

// SetGroupReviewHandler links the group review handler to enable navigation
// to the group review section from the dashboard.
func (h *Handler) SetGroupReviewHandler(reviewHandler interfaces.GroupReviewHandler) {
	h.groupReviewHandler = reviewHandler
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

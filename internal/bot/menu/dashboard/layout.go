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

// Layout handles the display and interaction logic for the main dashboard.
type Layout struct {
	db                *database.Client
	sessionManager    *session.Manager
	paginationManager *pagination.Manager
	workerMonitor     *core.Monitor
	mainMenu          *MainMenu
	logger            *zap.Logger
	userReviewLayout  interfaces.UserReviewLayout
	groupReviewLayout interfaces.GroupReviewLayout
	settingLayout     interfaces.SettingLayout
	logLayout         interfaces.LogLayout
	queueLayout       interfaces.QueueLayout
}

// New creates a Menu and sets up its page with message builders and
// interaction handlers. The page is configured to show statistics and
// handle navigation to other sections.
func New(
	db *database.Client,
	logger *zap.Logger,
	sessionManager *session.Manager,
	paginationManager *pagination.Manager,
	redisManager *redis.Manager,
) *Layout {
	// Get Redis client for worker status
	statusClient, err := redisManager.GetClient(redis.WorkerStatusDBIndex)
	if err != nil {
		logger.Fatal("Failed to get Redis client for worker status", zap.Error(err))
	}

	// Initialize layout
	l := &Layout{
		db:                db,
		logger:            logger,
		sessionManager:    sessionManager,
		paginationManager: paginationManager,
		workerMonitor:     core.NewMonitor(statusClient, logger),
	}
	l.mainMenu = NewMainMenu(l)

	// Initialize and register page
	paginationManager.AddPage(l.mainMenu.page)

	return l
}

// SetUserReviewLayout links the user review layout to enable navigation
// to the user review section from the dashboard.
func (l *Layout) SetUserReviewLayout(reviewLayout interfaces.UserReviewLayout) {
	l.userReviewLayout = reviewLayout
}

// SetGroupReviewLayout links the group review layout to enable navigation
// to the group review section from the dashboard.
func (l *Layout) SetGroupReviewLayout(reviewLayout interfaces.GroupReviewLayout) {
	l.groupReviewLayout = reviewLayout
}

// SetSettingLayout links the settings layout to enable navigation
// to the settings section from the dashboard.
func (l *Layout) SetSettingLayout(settingLayout interfaces.SettingLayout) {
	l.settingLayout = settingLayout
}

// SetLogLayout links the log layout to enable navigation
// to the logs section from the dashboard.
func (l *Layout) SetLogLayout(logLayout interfaces.LogLayout) {
	l.logLayout = logLayout
}

// SetQueueLayout links the queue layout to enable navigation
// to the queue section from the dashboard.
func (l *Layout) SetQueueLayout(queueLayout interfaces.QueueLayout) {
	l.queueLayout = queueLayout
}

// Show prepares and displays the dashboard interface by loading
// statistics and active user information into the session.
func (l *Layout) Show(event interfaces.CommonEvent, s *session.Session, content string) {
	l.mainMenu.Show(event, s, content)
}

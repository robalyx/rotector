package dashboard

import (
	"github.com/redis/rueidis"
	"github.com/rotector/rotector/internal/bot/core/pagination"
	"github.com/rotector/rotector/internal/bot/core/session"
	"github.com/rotector/rotector/internal/bot/interfaces"
	"github.com/rotector/rotector/internal/common/setup"
	"github.com/rotector/rotector/internal/common/storage/database"
	"github.com/rotector/rotector/internal/common/storage/redis"
	"github.com/rotector/rotector/internal/worker/core"
	"go.uber.org/zap"
)

// Layout handles the display and interaction logic for the main dashboard.
type Layout struct {
	db                *database.Client
	redisClient       rueidis.Client
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
	chatLayout        interfaces.ChatLayout
	appealLayout      interfaces.AppealLayout
}

// New creates a Menu and sets up its page with message builders and
// interaction handlers. The page is configured to show statistics and
// handle navigation to other sections.
func New(
	app *setup.App,
	sessionManager *session.Manager,
	paginationManager *pagination.Manager,
) *Layout {
	// Get Redis client for stats
	statsClient, err := app.RedisManager.GetClient(redis.StatsDBIndex)
	if err != nil {
		app.Logger.Fatal("Failed to get Redis client for stats", zap.Error(err))
	}

	// Get Redis client for worker status
	statusClient, err := app.RedisManager.GetClient(redis.WorkerStatusDBIndex)
	if err != nil {
		app.Logger.Fatal("Failed to get Redis client for worker status", zap.Error(err))
	}

	// Initialize layout
	l := &Layout{
		db:                app.DB,
		redisClient:       statsClient,
		logger:            app.Logger,
		sessionManager:    sessionManager,
		paginationManager: paginationManager,
		workerMonitor:     core.NewMonitor(statusClient, app.Logger),
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

// SetChatLayout links the chat layout to enable navigation
// to the AI chat section from the dashboard.
func (l *Layout) SetChatLayout(chatLayout interfaces.ChatLayout) {
	l.chatLayout = chatLayout
}

// SetAppealLayout links the appeal layout to enable navigation
// to the appeal section from the dashboard.
func (l *Layout) SetAppealLayout(appealLayout interfaces.AppealLayout) {
	l.appealLayout = appealLayout
}

// Show prepares and displays the dashboard interface by loading
// statistics and active user information into the session.
func (l *Layout) Show(event interfaces.CommonEvent, s *session.Session, content string) {
	l.mainMenu.Show(event, s, content)
}

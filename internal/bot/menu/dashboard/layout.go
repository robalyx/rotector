package dashboard

import (
	"github.com/redis/rueidis"
	"github.com/robalyx/rotector/internal/bot/core/pagination"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/interfaces"
	"github.com/robalyx/rotector/internal/common/setup"
	"github.com/robalyx/rotector/internal/common/storage/database"
	"github.com/robalyx/rotector/internal/common/storage/redis"
	"github.com/robalyx/rotector/internal/worker/core"
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
	adminLayout       interfaces.AdminLayout
	leaderboardLayout interfaces.LeaderboardLayout
	statusLayout      interfaces.StatusLayout
}

// New creates a Menu and sets up its page with message builders and
// interaction handlers. The page is configured to show statistics and
// handle navigation to other sections.
func New(
	app *setup.App,
	sessionManager *session.Manager,
	paginationManager *pagination.Manager,
	userReviewLayout interfaces.UserReviewLayout,
	groupReviewLayout interfaces.GroupReviewLayout,
	settingLayout interfaces.SettingLayout,
	logLayout interfaces.LogLayout,
	queueLayout interfaces.QueueLayout,
	chatLayout interfaces.ChatLayout,
	appealLayout interfaces.AppealLayout,
	adminLayout interfaces.AdminLayout,
	leaderboardLayout interfaces.LeaderboardLayout,
	statusLayout interfaces.StatusLayout,
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
		sessionManager:    sessionManager,
		paginationManager: paginationManager,
		logger:            app.Logger,
		workerMonitor:     core.NewMonitor(statusClient, app.Logger),
		userReviewLayout:  userReviewLayout,
		groupReviewLayout: groupReviewLayout,
		settingLayout:     settingLayout,
		logLayout:         logLayout,
		queueLayout:       queueLayout,
		chatLayout:        chatLayout,
		appealLayout:      appealLayout,
		adminLayout:       adminLayout,
		leaderboardLayout: leaderboardLayout,
		statusLayout:      statusLayout,
	}
	l.mainMenu = NewMainMenu(l)

	// Initialize and register page
	paginationManager.AddPage(l.mainMenu.page)

	return l
}

// Show prepares and displays the dashboard interface by loading
// statistics and active user information into the session.
func (l *Layout) Show(event interfaces.CommonEvent, s *session.Session, content string) {
	l.mainMenu.Show(event, s, content)
}

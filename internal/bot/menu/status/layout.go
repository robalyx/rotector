package status

import (
	"github.com/redis/rueidis"
	"github.com/robalyx/rotector/internal/bot/core/pagination"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/interfaces"
	"github.com/robalyx/rotector/internal/common/setup"
	"github.com/robalyx/rotector/internal/common/storage/redis"
	"github.com/robalyx/rotector/internal/worker/core"
	"go.uber.org/zap"
)

// Layout handles the display and interaction logic for the worker status menu.
type Layout struct {
	redisClient       rueidis.Client
	sessionManager    *session.Manager
	paginationManager *pagination.Manager
	workerMonitor     *core.Monitor
	mainMenu          *MainMenu
	logger            *zap.Logger
}

// New creates a Layout and sets up its page with message builders and
// interaction handlers.
func New(
	app *setup.App,
	sessionManager *session.Manager,
	paginationManager *pagination.Manager,
) interfaces.StatusLayout {
	// Get Redis client for worker status
	statusClient, err := app.RedisManager.GetClient(redis.WorkerStatusDBIndex)
	if err != nil {
		app.Logger.Fatal("Failed to get Redis client for worker status", zap.Error(err))
	}

	// Initialize layout
	l := &Layout{
		redisClient:       statusClient,
		sessionManager:    sessionManager,
		paginationManager: paginationManager,
		logger:            app.Logger,
		workerMonitor:     core.NewMonitor(statusClient, app.Logger),
	}
	l.mainMenu = NewMainMenu(l)

	// Initialize and register page
	paginationManager.AddPage(l.mainMenu.page)

	return l
}

// Show prepares and displays the worker status interface.
func (l *Layout) Show(event interfaces.CommonEvent, s *session.Session) {
	l.mainMenu.Show(event, s)
}

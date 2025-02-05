package dashboard

import (
	"github.com/redis/rueidis"
	"github.com/robalyx/rotector/internal/bot/core/pagination"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/common/setup"
	"github.com/robalyx/rotector/internal/common/storage/database"
	"github.com/robalyx/rotector/internal/common/storage/redis"
	"github.com/robalyx/rotector/internal/worker/core"
	"go.uber.org/zap"
)

// Layout handles the display and interaction logic for the main dashboard.
type Layout struct {
	db             database.Client
	redisClient    rueidis.Client
	sessionManager *session.Manager
	workerMonitor  *core.Monitor
	menu           *Menu
	logger         *zap.Logger
}

// New creates a Layout by initializing the dashboard menu.
func New(app *setup.App, sessionManager *session.Manager) *Layout {
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
		db:             app.DB,
		redisClient:    statsClient,
		sessionManager: sessionManager,
		logger:         app.Logger,
		workerMonitor:  core.NewMonitor(statusClient, app.Logger),
	}
	l.menu = NewMenu(l)

	return l
}

// Pages returns all the pages in the layout.
func (l *Layout) Pages() []*pagination.Page {
	return []*pagination.Page{
		l.menu.page,
	}
}

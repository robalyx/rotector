package status

import (
	"github.com/redis/rueidis"
	"github.com/robalyx/rotector/internal/bot/core/pagination"
	"github.com/robalyx/rotector/internal/common/setup"
	"github.com/robalyx/rotector/internal/common/storage/redis"
	"github.com/robalyx/rotector/internal/worker/core"
	"go.uber.org/zap"
)

// Layout handles the display and interaction logic for the worker status menu.
type Layout struct {
	redisClient   rueidis.Client
	workerMonitor *core.Monitor
	menu          *Menu
	logger        *zap.Logger
}

// New creates a Layout by initializing the worker status menu.
func New(app *setup.App) *Layout {
	// Get Redis client for worker status
	statusClient, err := app.RedisManager.GetClient(redis.WorkerStatusDBIndex)
	if err != nil {
		app.Logger.Fatal("Failed to get Redis client for worker status", zap.Error(err))
	}

	// Initialize layout
	l := &Layout{
		redisClient:   statusClient,
		logger:        app.Logger.Named("status_menu"),
		workerMonitor: core.NewMonitor(statusClient, app.Logger),
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

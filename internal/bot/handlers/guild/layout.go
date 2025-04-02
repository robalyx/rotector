package guild

import (
	"github.com/diamondburned/arikawa/v3/state"
	"github.com/redis/rueidis"
	"github.com/robalyx/rotector/internal/bot/core/interaction"
	"github.com/robalyx/rotector/internal/database"
	"github.com/robalyx/rotector/internal/discord"
	"github.com/robalyx/rotector/internal/redis"
	"github.com/robalyx/rotector/internal/setup"
	"go.uber.org/zap"
)

// Layout handles the display and interaction logic for the guild menu.
type Layout struct {
	db        database.Client
	client    *state.State
	ratelimit rueidis.Client
	scanner   *discord.Scanner
	menu      *Menu
	scan      *ScanMenu
	logs      *LogsMenu
	lookup    *LookupMenu
	messages  *MessagesMenu
	logger    *zap.Logger
}

// New creates a Layout by initializing the guild menu.
func New(app *setup.App, client *state.State) *Layout {
	ratelimit, err := app.RedisManager.GetClient(redis.RatelimitDBIndex)
	if err != nil {
		app.Logger.Fatal("Failed to get Redis client for rate limiting", zap.Error(err))
	}

	l := &Layout{
		db:        app.DB,
		client:    client,
		ratelimit: ratelimit,
		scanner:   discord.NewScanner(app.DB, ratelimit, client.Session, app.Logger),
		logger:    app.Logger.Named("guild_menu"),
	}

	l.menu = NewMenu(l)
	l.scan = NewScanMenu(l)
	l.logs = NewLogsMenu(l)
	l.lookup = NewLookupMenu(l)
	l.messages = NewMessagesMenu(l)

	return l
}

// Pages returns all the pages in the layout.
func (l *Layout) Pages() []*interaction.Page {
	return []*interaction.Page{
		l.menu.page,
		l.scan.page,
		l.logs.page,
		l.lookup.page,
		l.messages.page,
	}
}

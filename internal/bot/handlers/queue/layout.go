package queue

import (
	"github.com/robalyx/rotector/internal/bot/core/interaction"
	"github.com/robalyx/rotector/internal/cloudflare"
	"github.com/robalyx/rotector/internal/database"
	"github.com/robalyx/rotector/internal/roblox/fetcher"
	"github.com/robalyx/rotector/internal/setup"
	"go.uber.org/zap"
)

// Layout handles the display and interaction logic for the queue menu.
type Layout struct {
	db               database.Client
	d1Client         *cloudflare.Client
	userFetcher      *fetcher.UserFetcher
	groupFetcher     *fetcher.GroupFetcher
	thumbnailFetcher *fetcher.ThumbnailFetcher
	menu             *Menu
	logger           *zap.Logger
}

// New creates a Layout by initializing the queue menu.
func New(app *setup.App) *Layout {
	l := &Layout{
		db:               app.DB,
		d1Client:         app.D1Client,
		userFetcher:      fetcher.NewUserFetcher(app, app.Logger),
		groupFetcher:     fetcher.NewGroupFetcher(app.RoAPI, app.Logger),
		thumbnailFetcher: fetcher.NewThumbnailFetcher(app.RoAPI, app.Logger),
		logger:           app.Logger.Named("queue_menu"),
	}
	l.menu = NewMenu(l)

	return l
}

// Pages returns all the pages in the layout.
func (l *Layout) Pages() []*interaction.Page {
	return []*interaction.Page{
		l.menu.page,
	}
}

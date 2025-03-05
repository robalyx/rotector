package user

import (
	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/robalyx/rotector/internal/bot/core/captcha"
	"github.com/robalyx/rotector/internal/bot/core/pagination"
	"github.com/robalyx/rotector/internal/common/client/fetcher"
	"github.com/robalyx/rotector/internal/common/queue"
	"github.com/robalyx/rotector/internal/common/setup"
	"github.com/robalyx/rotector/internal/common/storage/database"
	"github.com/robalyx/rotector/internal/common/translator"
	"go.uber.org/zap"
)

// Layout handles all review-related menus and their interactions.
type Layout struct {
	db               database.Client
	roAPI            *api.API
	queueManager     *queue.Manager
	translator       *translator.Translator
	reviewMenu       *ReviewMenu
	outfitsMenu      *OutfitsMenu
	friendsMenu      *FriendsMenu
	groupsMenu       *GroupsMenu
	statusMenu       *StatusMenu
	thumbnailFetcher *fetcher.ThumbnailFetcher
	presenceFetcher  *fetcher.PresenceFetcher
	imageStreamer    *pagination.ImageStreamer
	captcha          *captcha.Manager
	logger           *zap.Logger
}

// New creates a Layout by initializing all review menus.
func New(app *setup.App, paginationManager *pagination.Manager) *Layout {
	// Initialize layout
	l := &Layout{
		db:               app.DB,
		roAPI:            app.RoAPI,
		queueManager:     app.Queue,
		translator:       translator.New(app.RoAPI.GetClient()),
		thumbnailFetcher: fetcher.NewThumbnailFetcher(app.RoAPI, app.Logger),
		presenceFetcher:  fetcher.NewPresenceFetcher(app.RoAPI, app.Logger),
		imageStreamer:    pagination.NewImageStreamer(paginationManager, app.Logger, app.RoAPI.GetClient()),
		captcha:          captcha.NewManager(app.DB, app.Logger),
		logger:           app.Logger.Named("user_menu"),
	}

	// Initialize all menus with references to this layout
	l.reviewMenu = NewReviewMenu(l)
	l.outfitsMenu = NewOutfitsMenu(l)
	l.friendsMenu = NewFriendsMenu(l)
	l.groupsMenu = NewGroupsMenu(l)
	l.statusMenu = NewStatusMenu(l)

	return l
}

// Pages returns all the pages in the layout.
func (l *Layout) Pages() []*pagination.Page {
	return []*pagination.Page{
		l.reviewMenu.page,
		l.outfitsMenu.page,
		l.friendsMenu.page,
		l.groupsMenu.page,
		l.statusMenu.page,
	}
}

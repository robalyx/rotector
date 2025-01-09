package user

import (
	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/robalyx/rotector/internal/bot/core/pagination"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/interfaces"
	"github.com/robalyx/rotector/internal/common/client/fetcher"
	"github.com/robalyx/rotector/internal/common/queue"
	"github.com/robalyx/rotector/internal/common/setup"
	"github.com/robalyx/rotector/internal/common/storage/database"
	"github.com/robalyx/rotector/internal/common/translator"
	"go.uber.org/zap"
)

// Layout handles all review-related menus and their interactions.
type Layout struct {
	db                *database.Client
	roAPI             *api.API
	sessionManager    *session.Manager
	paginationManager *pagination.Manager
	queueManager      *queue.Manager
	translator        *translator.Translator
	reviewMenu        *ReviewMenu
	outfitsMenu       *OutfitsMenu
	friendsMenu       *FriendsMenu
	groupsMenu        *GroupsMenu
	statusMenu        *StatusMenu
	thumbnailFetcher  *fetcher.ThumbnailFetcher
	presenceFetcher   *fetcher.PresenceFetcher
	imageStreamer     *pagination.ImageStreamer
	logger            *zap.Logger
	settingLayout     interfaces.SettingLayout
	logLayout         interfaces.LogLayout
	chatLayout        interfaces.ChatLayout
	captchaLayout     interfaces.CaptchaLayout
}

// New creates a Layout by initializing all review menus and registering their
// pages with the pagination manager.
func New(
	app *setup.App,
	sessionManager *session.Manager,
	paginationManager *pagination.Manager,
	settingLayout interfaces.SettingLayout,
	logLayout interfaces.LogLayout,
	chatLayout interfaces.ChatLayout,
	captchaLayout interfaces.CaptchaLayout,
) *Layout {
	// Initialize layout
	l := &Layout{
		db:                app.DB,
		roAPI:             app.RoAPI,
		sessionManager:    sessionManager,
		paginationManager: paginationManager,
		queueManager:      app.Queue,
		translator:        translator.New(app.RoAPI.GetClient()),
		thumbnailFetcher:  fetcher.NewThumbnailFetcher(app.RoAPI, app.Logger),
		presenceFetcher:   fetcher.NewPresenceFetcher(app.RoAPI, app.Logger),
		imageStreamer:     pagination.NewImageStreamer(paginationManager, app.Logger, app.RoAPI.GetClient()),
		logger:            app.Logger,
		settingLayout:     settingLayout,
		logLayout:         logLayout,
		chatLayout:        chatLayout,
		captchaLayout:     captchaLayout,
	}

	// Initialize all menus with references to this layout
	l.reviewMenu = NewReviewMenu(l)
	l.outfitsMenu = NewOutfitsMenu(l)
	l.friendsMenu = NewFriendsMenu(l)
	l.groupsMenu = NewGroupsMenu(l)
	l.statusMenu = NewStatusMenu(l)

	// Register menu pages with the pagination manager
	paginationManager.AddPage(l.reviewMenu.page)
	paginationManager.AddPage(l.outfitsMenu.page)
	paginationManager.AddPage(l.friendsMenu.page)
	paginationManager.AddPage(l.groupsMenu.page)
	paginationManager.AddPage(l.statusMenu.page)

	return l
}

// ShowReviewMenu prepares and displays the review interface by loading
// user data and friend status information into the session.
func (l *Layout) ShowReviewMenu(event interfaces.CommonEvent, s *session.Session) {
	l.reviewMenu.Show(event, s, "")
}

// ShowStatusMenu prepares and displays the status interface by loading
// current queue counts and position information into the session.
func (l *Layout) ShowStatusMenu(event interfaces.CommonEvent, s *session.Session) {
	l.statusMenu.Show(event, s)
}

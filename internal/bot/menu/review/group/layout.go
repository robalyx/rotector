package group

import (
	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/robalyx/rotector/internal/bot/core/captcha"
	"github.com/robalyx/rotector/internal/bot/core/pagination"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/interfaces"
	"github.com/robalyx/rotector/internal/common/client/fetcher"
	"github.com/robalyx/rotector/internal/common/setup"
	"github.com/robalyx/rotector/internal/common/storage/database"
	"go.uber.org/zap"
)

// Layout handles all review-related menus and their interactions.
type Layout struct {
	db                database.Client
	roAPI             *api.API
	sessionManager    *session.Manager
	paginationManager *pagination.Manager
	reviewMenu        *ReviewMenu
	membersMenu       *MembersMenu
	groupFetcher      *fetcher.GroupFetcher
	thumbnailFetcher  *fetcher.ThumbnailFetcher
	presenceFetcher   *fetcher.PresenceFetcher
	imageStreamer     *pagination.ImageStreamer
	captcha           *captcha.Manager
	logger            *zap.Logger
	settingLayout     interfaces.SettingLayout
	logLayout         interfaces.LogLayout
	chatLayout        interfaces.ChatLayout
	captchaLayout     interfaces.CaptchaLayout
}

// New creates a Layout by initializing all review menus and registering their
// pages with the pagination manager. The layout is configured with references
// to all required services and managers.
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
		groupFetcher:      fetcher.NewGroupFetcher(app.RoAPI, app.Logger),
		thumbnailFetcher:  fetcher.NewThumbnailFetcher(app.RoAPI, app.Logger),
		presenceFetcher:   fetcher.NewPresenceFetcher(app.RoAPI, app.Logger),
		imageStreamer:     pagination.NewImageStreamer(paginationManager, app.Logger, app.RoAPI.GetClient()),
		captcha:           captcha.NewManager(app.DB, app.Logger),
		logger:            app.Logger,
		settingLayout:     settingLayout,
		logLayout:         logLayout,
		chatLayout:        chatLayout,
		captchaLayout:     captchaLayout,
	}

	// Initialize all menus with references to this layout
	l.reviewMenu = NewReviewMenu(l)
	l.membersMenu = NewMembersMenu(l)

	// Register menu pages with the pagination manager
	paginationManager.AddPage(l.reviewMenu.page)
	paginationManager.AddPage(l.membersMenu.page)

	return l
}

// Show prepares and displays the review interface by loading
// group data and review settings into the session.
func (l *Layout) Show(event interfaces.CommonEvent, s *session.Session) {
	l.reviewMenu.Show(event, s, "")
}

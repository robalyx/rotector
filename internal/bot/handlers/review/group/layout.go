package group

import (
	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/captcha"
	"github.com/robalyx/rotector/internal/bot/core/interaction"
	"github.com/robalyx/rotector/internal/bot/handlers/review/shared"
	sharedView "github.com/robalyx/rotector/internal/bot/views/review/shared"
	"github.com/robalyx/rotector/internal/database"
	"github.com/robalyx/rotector/internal/roblox/fetcher"
	"github.com/robalyx/rotector/internal/setup"
	"go.uber.org/zap"
)

// Layout handles all review-related menus and their interactions.
type Layout struct {
	db               database.Client
	roAPI            *api.API
	reviewMenu       *ReviewMenu
	membersMenu      *MembersMenu
	commentsMenu     *shared.CommentsMenu
	thumbnailFetcher *fetcher.ThumbnailFetcher
	presenceFetcher  *fetcher.PresenceFetcher
	imageStreamer    *interaction.ImageStreamer
	captcha          *captcha.Manager
	logger           *zap.Logger
}

// New creates a Layout by initializing all review menus.
func New(app *setup.App, interactionManager *interaction.Manager) *Layout {
	// Initialize layout
	l := &Layout{
		db:               app.DB,
		roAPI:            app.RoAPI,
		thumbnailFetcher: fetcher.NewThumbnailFetcher(app.RoAPI, app.Logger),
		presenceFetcher:  fetcher.NewPresenceFetcher(app.RoAPI, app.Logger),
		imageStreamer:    interaction.NewImageStreamer(interactionManager, app.Logger, app.RoAPI.GetClient()),
		captcha:          captcha.NewManager(app.DB, app.Logger),
		logger:           app.Logger.Named("group_menu"),
	}

	// Initialize all menus with references to this layout
	l.reviewMenu = NewReviewMenu(l)
	l.membersMenu = NewMembersMenu(l)
	l.commentsMenu = shared.NewCommentsMenu(l.logger, l.db, sharedView.TargetTypeGroup, constants.GroupCommentsPageName)

	return l
}

// Pages returns all the pages in the layout.
func (l *Layout) Pages() []*interaction.Page {
	return []*interaction.Page{
		l.reviewMenu.page,
		l.membersMenu.page,
		l.commentsMenu.Page(),
	}
}

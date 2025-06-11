package user

import (
	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/robalyx/rotector/internal/ai"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/captcha"
	"github.com/robalyx/rotector/internal/bot/core/interaction"
	"github.com/robalyx/rotector/internal/bot/handlers/review/shared"
	sharedView "github.com/robalyx/rotector/internal/bot/views/review/shared"
	"github.com/robalyx/rotector/internal/database"
	"github.com/robalyx/rotector/internal/roblox/checker"
	"github.com/robalyx/rotector/internal/roblox/fetcher"
	"github.com/robalyx/rotector/internal/setup"
	"github.com/robalyx/rotector/internal/translator"
	"go.uber.org/zap"
)

// Layout handles all review-related menus and their interactions.
type Layout struct {
	db                   database.Client
	roAPI                *api.API
	translator           *translator.Translator
	reviewMenu           *ReviewMenu
	outfitsMenu          *OutfitsMenu
	friendsMenu          *FriendsMenu
	groupsMenu           *GroupsMenu
	caesarMenu           *CaesarMenu
	commentsMenu         *shared.CommentsMenu
	thumbnailFetcher     *fetcher.ThumbnailFetcher
	presenceFetcher      *fetcher.PresenceFetcher
	friendChecker        *checker.FriendChecker
	groupChecker         *checker.GroupChecker
	friendReasonAnalyzer *ai.FriendReasonAnalyzer
	groupReasonAnalyzer  *ai.GroupReasonAnalyzer
	imageStreamer        *interaction.ImageStreamer
	captcha              *captcha.Manager
	logger               *zap.Logger
}

// New creates a Layout by initializing all review menus.
func New(app *setup.App, interactionManager *interaction.Manager) *Layout {
	// Initialize layout
	l := &Layout{
		db:                   app.DB,
		roAPI:                app.RoAPI,
		translator:           translator.New(app.RoAPI.GetClient()),
		thumbnailFetcher:     fetcher.NewThumbnailFetcher(app.RoAPI, app.Logger),
		presenceFetcher:      fetcher.NewPresenceFetcher(app.RoAPI, app.Logger),
		friendChecker:        checker.NewFriendChecker(app, app.Logger),
		groupChecker:         checker.NewGroupChecker(app, app.Logger),
		friendReasonAnalyzer: ai.NewFriendReasonAnalyzer(app, app.Logger),
		groupReasonAnalyzer:  ai.NewGroupReasonAnalyzer(app, app.Logger),
		imageStreamer:        interaction.NewImageStreamer(interactionManager, app.Logger, app.RoAPI.GetClient()),
		captcha:              captcha.NewManager(app.DB, app.Logger),
		logger:               app.Logger.Named("user_menu"),
	}

	// Initialize all menus with references to this layout
	l.reviewMenu = NewReviewMenu(l)
	l.outfitsMenu = NewOutfitsMenu(l)
	l.friendsMenu = NewFriendsMenu(l)
	l.groupsMenu = NewGroupsMenu(l)
	l.caesarMenu = NewCaesarMenu(l)
	l.commentsMenu = shared.NewCommentsMenu(l.logger, l.db, sharedView.TargetTypeUser, constants.UserCommentsPageName)

	return l
}

// Pages returns all the pages in the layout.
func (l *Layout) Pages() []*interaction.Page {
	return []*interaction.Page{
		l.reviewMenu.page,
		l.outfitsMenu.page,
		l.friendsMenu.page,
		l.groupsMenu.page,
		l.caesarMenu.page,
		l.commentsMenu.Page(),
	}
}

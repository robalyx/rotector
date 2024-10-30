package review

import (
	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/rotector/rotector/internal/bot/interfaces"
	"github.com/rotector/rotector/internal/bot/pagination"
	"github.com/rotector/rotector/internal/bot/session"
	"github.com/rotector/rotector/internal/common/database"
	"go.uber.org/zap"
)

// Handler manages the review process for users.
type Handler struct {
	db                *database.Database
	roAPI             *api.API
	sessionManager    *session.Manager
	paginationManager *pagination.Manager
	reviewMenu        *Menu
	outfitsMenu       *OutfitsMenu
	friendsMenu       *FriendsMenu
	groupsMenu        *GroupsMenu
	logger            *zap.Logger
	dashboardHandler  interfaces.DashboardHandler
}

// New creates a new Handler instance.
func New(db *database.Database, logger *zap.Logger, roAPI *api.API, sessionManager *session.Manager, paginationManager *pagination.Manager, dashboardHandler interfaces.DashboardHandler) *Handler {
	h := &Handler{
		db:                db,
		roAPI:             roAPI,
		sessionManager:    sessionManager,
		logger:            logger,
		paginationManager: paginationManager,
		dashboardHandler:  dashboardHandler,
	}

	h.reviewMenu = NewMenu(h)
	h.outfitsMenu = NewOutfitsMenu(h)
	h.friendsMenu = NewFriendsMenu(h)
	h.groupsMenu = NewGroupsMenu(h)

	paginationManager.AddPage(h.reviewMenu.page)
	paginationManager.AddPage(h.outfitsMenu.page)
	paginationManager.AddPage(h.friendsMenu.page)
	paginationManager.AddPage(h.groupsMenu.page)

	return h
}

// ShowReviewMenuAndFetchUser displays the review menu and fetches a new user.
func (h *Handler) ShowReviewMenuAndFetchUser(event interfaces.CommonEvent, s *session.Session, content string) {
	h.reviewMenu.ShowReviewMenuAndFetchUser(event, s, content)
}

package review

import (
	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/rotector/rotector/internal/bot/interfaces"
	"github.com/rotector/rotector/internal/bot/pagination"
	"github.com/rotector/rotector/internal/bot/session"
	"github.com/rotector/rotector/internal/common/database"
	"github.com/rotector/rotector/internal/common/queue"
	"go.uber.org/zap"
)

// Handler coordinates all review-related menus and their interactions.
// It maintains references to the API clients, database, and managers needed
// for processing user reviews and queue operations.
type Handler struct {
	db                *database.Database
	roAPI             *api.API
	sessionManager    *session.Manager
	paginationManager *pagination.Manager
	queueManager      *queue.Manager
	reviewMenu        *Menu
	outfitsMenu       *OutfitsMenu
	friendsMenu       *FriendsMenu
	groupsMenu        *GroupsMenu
	statusMenu        *StatusMenu
	logger            *zap.Logger
	dashboardHandler  interfaces.DashboardHandler
}

// New creates a Handler by initializing all review menus and registering their
// pages with the pagination manager.
func New(
	db *database.Database,
	logger *zap.Logger,
	roAPI *api.API,
	sessionManager *session.Manager,
	paginationManager *pagination.Manager,
	queueManager *queue.Manager,
	dashboardHandler interfaces.DashboardHandler,
) *Handler {
	h := &Handler{
		db:                db,
		roAPI:             roAPI,
		sessionManager:    sessionManager,
		paginationManager: paginationManager,
		queueManager:      queueManager,
		logger:            logger,
		dashboardHandler:  dashboardHandler,
	}

	// Initialize all menus with references to this handler
	h.reviewMenu = NewMenu(h)
	h.outfitsMenu = NewOutfitsMenu(h)
	h.friendsMenu = NewFriendsMenu(h)
	h.groupsMenu = NewGroupsMenu(h)
	h.statusMenu = NewStatusMenu(h)

	// Register menu pages with the pagination manager
	paginationManager.AddPage(h.reviewMenu.page)
	paginationManager.AddPage(h.outfitsMenu.page)
	paginationManager.AddPage(h.friendsMenu.page)
	paginationManager.AddPage(h.groupsMenu.page)
	paginationManager.AddPage(h.statusMenu.page)

	return h
}

// ShowReviewMenu prepares and displays the review interface by loading
// user data and friend status information into the session.
func (h *Handler) ShowReviewMenu(event interfaces.CommonEvent, s *session.Session) {
	h.reviewMenu.ShowReviewMenu(event, s, "")
}

// ShowStatusMenu prepares and displays the status interface by loading
// current queue counts and position information into the session.
func (h *Handler) ShowStatusMenu(event interfaces.CommonEvent, s *session.Session) {
	h.statusMenu.ShowStatusMenu(event, s)
}

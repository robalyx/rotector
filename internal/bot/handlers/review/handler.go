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

// Handler manages the review process for users.
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

// New creates a new Handler instance.
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

	h.reviewMenu = NewMenu(h)
	h.outfitsMenu = NewOutfitsMenu(h)
	h.friendsMenu = NewFriendsMenu(h)
	h.groupsMenu = NewGroupsMenu(h)
	h.statusMenu = NewStatusMenu(h)

	paginationManager.AddPage(h.reviewMenu.page)
	paginationManager.AddPage(h.outfitsMenu.page)
	paginationManager.AddPage(h.friendsMenu.page)
	paginationManager.AddPage(h.groupsMenu.page)
	paginationManager.AddPage(h.statusMenu.page)

	return h
}

// ShowReviewMenuAndFetchUser displays the review menu and fetches a new user.
func (h *Handler) ShowReviewMenuAndFetchUser(event interfaces.CommonEvent, s *session.Session, content string) {
	h.reviewMenu.ShowReviewMenuAndFetchUser(event, s, content)
}

// ShowStatusMenu shows the status menu.
func (h *Handler) ShowStatusMenu(event interfaces.CommonEvent, s *session.Session) {
	h.statusMenu.ShowStatusMenu(event, s)
}

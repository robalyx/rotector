package group

import (
	"github.com/jaxron/roapi.go/pkg/api"
	"github.com/rotector/rotector/internal/bot/core/pagination"
	"github.com/rotector/rotector/internal/bot/core/session"
	"github.com/rotector/rotector/internal/bot/interfaces"
	"github.com/rotector/rotector/internal/common/client/fetcher"
	"github.com/rotector/rotector/internal/common/storage/database"
	"go.uber.org/zap"
)

// Handler coordinates all review-related menus and their interactions.
// It maintains references to the database, managers, and handlers needed
// for processing group reviews and logging operations.
type Handler struct {
	db                *database.Client
	sessionManager    *session.Manager
	paginationManager *pagination.Manager
	reviewMenu        *Menu
	logger            *zap.Logger
	dashboardHandler  interfaces.DashboardHandler
	logHandler        interfaces.LogHandler
	groupFetcher      *fetcher.GroupFetcher
}

// New creates a Handler by initializing all review menus and registering their
// pages with the pagination manager. The handler is configured with references
// to all required services and managers.
func New(
	db *database.Client,
	logger *zap.Logger,
	roAPI *api.API,
	sessionManager *session.Manager,
	paginationManager *pagination.Manager,
	dashboardHandler interfaces.DashboardHandler,
	logHandler interfaces.LogHandler,
) *Handler {
	h := &Handler{
		db:                db,
		sessionManager:    sessionManager,
		paginationManager: paginationManager,
		logger:            logger,
		dashboardHandler:  dashboardHandler,
		logHandler:        logHandler,
		groupFetcher:      fetcher.NewGroupFetcher(roAPI, logger),
	}

	// Initialize review menu with reference to this handler
	h.reviewMenu = NewMenu(h)

	// Register menu page with the pagination manager for navigation
	paginationManager.AddPage(h.reviewMenu.page)

	return h
}

// ShowReviewMenu prepares and displays the review interface by loading
// group data and review settings into the session.
func (h *Handler) ShowReviewMenu(event interfaces.CommonEvent, s *session.Session) {
	h.reviewMenu.ShowReviewMenu(event, s, "")
}

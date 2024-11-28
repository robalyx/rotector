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

// Layout handles all review-related menus and their interactions.
type Layout struct {
	db                *database.Client
	sessionManager    *session.Manager
	paginationManager *pagination.Manager
	reviewMenu        *ReviewMenu
	logger            *zap.Logger
	dashboardLayout   interfaces.DashboardLayout
	logLayout         interfaces.LogLayout
	groupFetcher      *fetcher.GroupFetcher
}

// New creates a Layout by initializing all review menus and registering their
// pages with the pagination manager. The layout is configured with references
// to all required services and managers.
func New(
	db *database.Client,
	logger *zap.Logger,
	roAPI *api.API,
	sessionManager *session.Manager,
	paginationManager *pagination.Manager,
	dashboardLayout interfaces.DashboardLayout,
	logLayout interfaces.LogLayout,
) *Layout {
	// Initialize layout
	l := &Layout{
		db:                db,
		sessionManager:    sessionManager,
		paginationManager: paginationManager,
		logger:            logger,
		dashboardLayout:   dashboardLayout,
		logLayout:         logLayout,
		groupFetcher:      fetcher.NewGroupFetcher(roAPI, logger),
	}
	l.reviewMenu = NewReviewMenu(l)

	// Register menu page with the pagination manager
	paginationManager.AddPage(l.reviewMenu.page)

	return l
}

// Show prepares and displays the review interface by loading
// group data and review settings into the session.
func (l *Layout) Show(event interfaces.CommonEvent, s *session.Session) {
	l.reviewMenu.Show(event, s, "")
}

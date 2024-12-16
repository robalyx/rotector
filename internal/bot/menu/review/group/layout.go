package group

import (
	"github.com/rotector/rotector/internal/bot/core/pagination"
	"github.com/rotector/rotector/internal/bot/core/session"
	"github.com/rotector/rotector/internal/bot/interfaces"
	"github.com/rotector/rotector/internal/common/client/fetcher"
	"github.com/rotector/rotector/internal/common/setup"
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
	settingLayout     interfaces.SettingLayout
	logLayout         interfaces.LogLayout
	chatLayout        interfaces.ChatLayout
	groupFetcher      *fetcher.GroupFetcher
}

// New creates a Layout by initializing all review menus and registering their
// pages with the pagination manager. The layout is configured with references
// to all required services and managers.
func New(
	app *setup.App,
	sessionManager *session.Manager,
	paginationManager *pagination.Manager,
	dashboardLayout interfaces.DashboardLayout,
	settingLayout interfaces.SettingLayout,
	logLayout interfaces.LogLayout,
	chatLayout interfaces.ChatLayout,
) *Layout {
	// Initialize layout
	l := &Layout{
		db:                app.DB,
		sessionManager:    sessionManager,
		paginationManager: paginationManager,
		logger:            app.Logger,
		dashboardLayout:   dashboardLayout,
		settingLayout:     settingLayout,
		logLayout:         logLayout,
		chatLayout:        chatLayout,
		groupFetcher:      fetcher.NewGroupFetcher(app.RoAPI, app.Logger),
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

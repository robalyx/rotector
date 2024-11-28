package log

import (
	"time"

	"github.com/rotector/rotector/internal/bot/constants"
	"github.com/rotector/rotector/internal/bot/core/pagination"
	"github.com/rotector/rotector/internal/bot/core/session"
	"github.com/rotector/rotector/internal/bot/interfaces"
	"github.com/rotector/rotector/internal/common/storage/database"
	"github.com/rotector/rotector/internal/common/storage/database/models"
	"go.uber.org/zap"
)

// Layout handles log viewing operations and their interactions.
type Layout struct {
	db                *database.Client
	sessionManager    *session.Manager
	paginationManager *pagination.Manager
	mainMenu          *MainMenu
	logger            *zap.Logger
	dashboardLayout   interfaces.DashboardLayout
}

// New creates a Layout by initializing the log menu and registering its
// page with the pagination manager.
func New(
	db *database.Client,
	sessionManager *session.Manager,
	paginationManager *pagination.Manager,
	dashboardLayout interfaces.DashboardLayout,
	logger *zap.Logger,
) *Layout {
	// Initialize layout
	l := &Layout{
		db:                db,
		sessionManager:    sessionManager,
		paginationManager: paginationManager,
		dashboardLayout:   dashboardLayout,
		logger:            logger,
	}
	l.mainMenu = NewMainMenu(l)

	// Initialize and register page
	paginationManager.AddPage(l.mainMenu.page)

	return l
}

// Show prepares and displays the log interface by initializing
// session data with default values and loading user preferences.
func (l *Layout) Show(event interfaces.CommonEvent, s *session.Session) {
	l.mainMenu.Show(event, s)
}

// ResetFilters resets all log filters to their default values in the given session.
func (l *Layout) ResetFilters(s *session.Session) {
	s.Set(constants.SessionKeyLogs, []*models.UserActivityLog{})
	s.Set(constants.SessionKeyUserIDFilter, uint64(0))
	s.Set(constants.SessionKeyGroupIDFilter, uint64(0))
	s.Set(constants.SessionKeyReviewerIDFilter, uint64(0))
	s.Set(constants.SessionKeyActivityTypeFilter, models.ActivityTypeAll)
	s.Set(constants.SessionKeyDateRangeStartFilter, time.Time{})
	s.Set(constants.SessionKeyDateRangeEndFilter, time.Time{})
	s.Set(constants.SessionKeyCursor, nil)
	s.Set(constants.SessionKeyNextCursor, nil)
	s.Set(constants.SessionKeyPrevCursors, []*models.LogCursor{})
	s.Set(constants.SessionKeyHasNextPage, false)
	s.Set(constants.SessionKeyHasPrevPage, false)
}

package log

import (
	"github.com/robalyx/rotector/internal/bot/core/pagination"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/interfaces"
	"github.com/robalyx/rotector/internal/common/setup"
	"github.com/robalyx/rotector/internal/common/storage/database"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
	"go.uber.org/zap"
)

// Layout handles log viewing operations and their interactions.
type Layout struct {
	db                database.Client
	sessionManager    *session.Manager
	paginationManager *pagination.Manager
	mainMenu          *MainMenu
	logger            *zap.Logger
}

// New creates a Layout by initializing the log menu and registering its
// page with the pagination manager.
func New(
	app *setup.App,
	sessionManager *session.Manager,
	paginationManager *pagination.Manager,
) *Layout {
	// Initialize layout
	l := &Layout{
		db:                app.DB,
		sessionManager:    sessionManager,
		paginationManager: paginationManager,
		logger:            app.Logger,
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
	session.LogFilterDiscordID.Delete(s)
	session.LogFilterUserID.Delete(s)
	session.LogFilterGroupID.Delete(s)
	session.LogFilterReviewerID.Delete(s)
	session.LogFilterActivityType.Set(s, enum.ActivityTypeAll)
	session.LogFilterActivityCategory.Delete(s)
	session.LogFilterDateRangeStart.Delete(s)
	session.LogFilterDateRangeEnd.Delete(s)
}

// ResetLogs clears the logs from the session.
func (l *Layout) ResetLogs(s *session.Session) {
	session.LogActivities.Delete(s)
	session.LogCursor.Delete(s)
	session.LogNextCursor.Delete(s)
	session.LogPrevCursors.Delete(s)
	session.PaginationHasNextPage.Delete(s)
	session.PaginationHasPrevPage.Delete(s)
}

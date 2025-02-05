package log

import (
	"github.com/robalyx/rotector/internal/bot/core/pagination"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/common/setup"
	"github.com/robalyx/rotector/internal/common/storage/database"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
	"go.uber.org/zap"
)

// Layout handles log viewing operations and their interactions.
type Layout struct {
	db     database.Client
	menu   *Menu
	logger *zap.Logger
}

// New creates a Layout by initializing the log menu.
func New(app *setup.App) *Layout {
	l := &Layout{
		db:     app.DB,
		logger: app.Logger,
	}
	l.menu = NewMenu(l)

	return l
}

// Pages returns all the pages in the layout.
func (l *Layout) Pages() []*pagination.Page {
	return []*pagination.Page{
		l.menu.page,
	}
}

// ResetFilters resets all log filters to their default values in the given session.
func ResetFilters(s *session.Session) {
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
func ResetLogs(s *session.Session) {
	session.LogActivities.Delete(s)
	session.LogCursor.Delete(s)
	session.LogNextCursor.Delete(s)
	session.LogPrevCursors.Delete(s)
	session.PaginationHasNextPage.Delete(s)
	session.PaginationHasPrevPage.Delete(s)
}

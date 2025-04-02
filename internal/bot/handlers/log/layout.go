package log

import (
	"github.com/robalyx/rotector/internal/bot/core/interaction"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/database"
	"github.com/robalyx/rotector/internal/database/types/enum"
	"github.com/robalyx/rotector/internal/setup"
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
		logger: app.Logger.Named("log_menu"),
	}
	l.menu = NewMenu(l)

	return l
}

// Pages returns all the pages in the layout.
func (l *Layout) Pages() []*interaction.Page {
	return []*interaction.Page{
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

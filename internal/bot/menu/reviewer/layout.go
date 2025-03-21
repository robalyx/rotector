package reviewer

import (
	"github.com/disgoorg/disgo/bot"
	"github.com/robalyx/rotector/internal/bot/core/interaction"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/common/setup"
	"github.com/robalyx/rotector/internal/common/storage/database"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"go.uber.org/zap"
)

// Layout handles reviewer stats operations and their interactions.
type Layout struct {
	db     database.Client
	client bot.Client
	menu   *Menu
	logger *zap.Logger
}

// New creates a Layout by initializing the reviewer stats menu.
func New(app *setup.App, client bot.Client) *Layout {
	l := &Layout{
		db:     app.DB,
		client: client,
		logger: app.Logger.Named("reviewer_menu"),
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

// ResetStats clears the reviewer stats from the session.
func ResetStats(s *session.Session) {
	session.ReviewerStats.Set(s, make(map[uint64]*types.ReviewerStats))
	session.ReviewerStatsCursor.Set(s, nil)
	session.ReviewerStatsNextCursor.Set(s, nil)
	session.ReviewerStatsPrevCursors.Set(s, []*types.ReviewerStatsCursor{})
	session.PaginationHasNextPage.Set(s, false)
	session.PaginationHasPrevPage.Set(s, false)
}

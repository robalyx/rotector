package leaderboard

import (
	"time"

	"github.com/disgoorg/disgo/bot"
	"github.com/robalyx/rotector/internal/bot/core/interaction"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/database"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/setup"
	"go.uber.org/zap"
)

// Layout handles leaderboard operations and their interactions.
type Layout struct {
	db     database.Client
	client bot.Client
	menu   *Menu
	logger *zap.Logger
}

// New creates a Layout by initializing the leaderboard menu.
func New(app *setup.App, client bot.Client) *Layout {
	l := &Layout{
		db:     app.DB,
		client: client,
		logger: app.Logger.Named("leaderboard_menu"),
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

// ResetStats clears the leaderboard stats from the session.
func ResetStats(s *session.Session) {
	session.LeaderboardStats.Set(s, []*types.VoteAccuracy{})
	session.LeaderboardUsernames.Set(s, make(map[uint64]string))
	session.LeaderboardCursor.Set(s, nil)
	session.LeaderboardNextCursor.Set(s, nil)
	session.LeaderboardPrevCursors.Set(s, []*types.LeaderboardCursor{})
	session.PaginationHasNextPage.Set(s, false)
	session.PaginationHasPrevPage.Set(s, false)
	session.LeaderboardLastRefresh.Set(s, time.Time{})
	session.LeaderboardNextRefresh.Set(s, time.Time{})
}

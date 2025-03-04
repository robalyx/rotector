package leaderboard

import (
	"context"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/snowflake/v2"
	builder "github.com/robalyx/rotector/internal/bot/builder/leaderboard"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/pagination"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/interfaces"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
	"go.uber.org/zap"
)

// Menu handles the display and interaction logic for viewing the leaderboard.
type Menu struct {
	layout *Layout
	page   *pagination.Page
}

// NewMenu creates a Menu and sets up its page with message builders and
// interaction handlers.
func NewMenu(l *Layout) *Menu {
	m := &Menu{layout: l}
	m.page = &pagination.Page{
		Name: constants.LeaderboardPageName,
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return builder.NewBuilder(s).Build()
		},
		ShowHandlerFunc:   m.Show,
		SelectHandlerFunc: m.handleSelectMenu,
		ButtonHandlerFunc: m.handleButton,
	}
	return m
}

// Show prepares and displays the leaderboard interface.
func (m *Menu) Show(event interfaces.CommonEvent, s *session.Session, r *pagination.Respond) {
	cursor := session.LeaderboardCursor.Get(s)
	period := session.UserLeaderboardPeriod.Get(s)

	// Fetch leaderboard stats from database
	stats, nextCursor, err := m.layout.db.Models().Votes().GetLeaderboard(
		context.Background(),
		period,
		cursor,
		constants.LeaderboardEntriesPerPage,
	)
	if err != nil {
		m.layout.logger.Error("Failed to get leaderboard stats", zap.Error(err))
		r.Error(event, "Failed to retrieve leaderboard data. Please try again.")
		return
	}

	// Get refresh info for leaderboard view
	lastRefresh, nextRefresh, err := m.layout.db.Models().Views().GetLeaderboardRefreshInfo(context.Background(), period)
	if err != nil {
		m.layout.logger.Error("Failed to get refresh info", zap.Error(err))
		r.Error(event, "Failed to retrieve leaderboard data. Please try again.")
		return
	}

	// Fetch usernames for all users in stats
	usernames := make(map[uint64]string)
	for _, stat := range stats {
		user, err := m.layout.client.Rest().GetUser(snowflake.ID(stat.DiscordUserID))
		if err != nil {
			usernames[stat.DiscordUserID] = "Unknown"
			continue
		}
		usernames[stat.DiscordUserID] = user.Username
	}

	// Store results in session
	session.LeaderboardStats.Set(s, stats)
	session.LeaderboardUsernames.Set(s, usernames)
	session.LeaderboardNextCursor.Set(s, nextCursor)
	session.PaginationHasNextPage.Set(s, nextCursor != nil)
	session.PaginationHasPrevPage.Set(s, cursor != nil)
	session.LeaderboardLastRefresh.Set(s, lastRefresh)
	session.LeaderboardNextRefresh.Set(s, nextRefresh)
}

// handleSelectMenu processes select menu interactions.
func (m *Menu) handleSelectMenu(
	event *events.ComponentInteractionCreate, s *session.Session, r *pagination.Respond, customID, option string,
) {
	if customID != constants.LeaderboardPeriodSelectMenuCustomID {
		return
	}

	// Parse option to leaderboard period
	period, err := enum.LeaderboardPeriodString(option)
	if err != nil {
		m.layout.logger.Error("Failed to parse leaderboard period", zap.Error(err))
		r.Error(event, "Failed to save time period preference. Please try again.")
		return
	}

	// Update user's leaderboard period preference
	session.UserLeaderboardPeriod.Set(s, period)

	// Reset page and show updated leaderboard
	ResetStats(s)
	r.Reload(event, s, "")
}

// handleButton processes button interactions.
func (m *Menu) handleButton(
	event *events.ComponentInteractionCreate, s *session.Session, r *pagination.Respond, customID string,
) {
	switch customID {
	case constants.BackButtonCustomID:
		r.NavigateBack(event, s, "")
	case constants.RefreshButtonCustomID:
		ResetStats(s)
		r.Reload(event, s, "")
	case string(session.ViewerFirstPage),
		string(session.ViewerPrevPage),
		string(session.ViewerNextPage),
		string(session.ViewerLastPage):
		m.handlePagination(event, s, r, session.ViewerAction(customID))
	}
}

// handlePagination processes page navigation.
func (m *Menu) handlePagination(
	event *events.ComponentInteractionCreate, s *session.Session, r *pagination.Respond, action session.ViewerAction,
) {
	switch action {
	case session.ViewerNextPage:
		cursor := session.LeaderboardCursor.Get(s)
		nextCursor := session.LeaderboardNextCursor.Get(s)
		prevCursors := session.LeaderboardPrevCursors.Get(s)

		if session.PaginationHasNextPage.Get(s) {
			session.LeaderboardCursor.Set(s, nextCursor)
			session.LeaderboardPrevCursors.Set(s, append(prevCursors, cursor))
			r.Reload(event, s, "")
		}
	case session.ViewerPrevPage:
		prevCursors := session.LeaderboardPrevCursors.Get(s)

		if len(prevCursors) > 0 {
			lastIdx := len(prevCursors) - 1
			session.LeaderboardPrevCursors.Set(s, prevCursors[:lastIdx])
			session.LeaderboardCursor.Set(s, prevCursors[lastIdx])
			r.Reload(event, s, "")
		}
	case session.ViewerFirstPage:
		session.LeaderboardCursor.Set(s, nil)
		session.LeaderboardPrevCursors.Set(s, []*types.LeaderboardCursor{})
		r.Reload(event, s, "")
	case session.ViewerLastPage:
		return
	}
}

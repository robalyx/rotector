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

// MainMenu handles the display and interaction logic for viewing the leaderboard.
type MainMenu struct {
	layout *Layout
	page   *pagination.Page
}

// NewMainMenu creates a MainMenu and sets up its page with message builders and
// interaction handlers.
func NewMainMenu(l *Layout) *MainMenu {
	m := &MainMenu{layout: l}
	m.page = &pagination.Page{
		Name: "Leaderboard Menu",
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return builder.NewBuilder(s).Build()
		},
		SelectHandlerFunc: m.handleSelectMenu,
		ButtonHandlerFunc: m.handleButton,
	}
	return m
}

// Show prepares and displays the leaderboard interface.
func (m *MainMenu) Show(event interfaces.CommonEvent, s *session.Session) {
	cursor := session.LeaderboardCursor.Get(s)
	leaderboardPeriod := session.UserLeaderboardPeriod.Get(s)

	// Fetch leaderboard stats from database
	stats, nextCursor, err := m.layout.db.Votes().GetLeaderboard(
		context.Background(),
		leaderboardPeriod,
		cursor,
		constants.LeaderboardEntriesPerPage,
	)
	if err != nil {
		m.layout.logger.Error("Failed to get leaderboard stats", zap.Error(err))
		m.layout.paginationManager.NavigateTo(event, s, m.page, "Failed to retrieve leaderboard data. Please try again.")
		return
	}

	// Get refresh info for leaderboard view
	lastRefresh, nextRefresh, err := m.layout.db.Views().GetRefreshInfo(context.Background(), leaderboardPeriod)
	if err != nil {
		m.layout.logger.Error("Failed to get refresh info", zap.Error(err))
	}

	// Fetch usernames for all users in stats
	usernames := make(map[uint64]string)
	for _, stat := range stats {
		if user, err := m.layout.client.Rest().GetUser(snowflake.ID(stat.DiscordUserID)); err == nil {
			usernames[stat.DiscordUserID] = user.Username
		}
	}

	// Store results in session
	session.LeaderboardStats.Set(s, stats)
	session.LeaderboardUsernames.Set(s, usernames)
	session.LeaderboardNextCursor.Set(s, nextCursor)
	session.PaginationHasNextPage.Set(s, nextCursor != nil)
	session.PaginationHasPrevPage.Set(s, cursor != nil)
	session.LeaderboardLastRefresh.Set(s, lastRefresh)
	session.LeaderboardNextRefresh.Set(s, nextRefresh)

	m.layout.paginationManager.NavigateTo(event, s, m.page, "")
}

// handleSelectMenu processes select menu interactions.
func (m *MainMenu) handleSelectMenu(event *events.ComponentInteractionCreate, s *session.Session, customID string, option string) {
	if customID != constants.LeaderboardPeriodSelectMenuCustomID {
		return
	}

	// Parse option to leaderboard period
	period, err := enum.LeaderboardPeriodString(option)
	if err != nil {
		m.layout.logger.Error("Failed to parse leaderboard period", zap.Error(err))
		m.layout.paginationManager.RespondWithError(event, "Failed to save time period preference. Please try again.")
		return
	}

	// Update user's leaderboard period preference
	session.UserLeaderboardPeriod.Set(s, period)

	// Reset page and show updated leaderboard
	m.layout.ResetStats(s)
	m.Show(event, s)
}

// handleButton processes button interactions.
func (m *MainMenu) handleButton(event *events.ComponentInteractionCreate, s *session.Session, customID string) {
	switch customID {
	case constants.BackButtonCustomID:
		m.layout.paginationManager.NavigateBack(event, s, "")
	case constants.RefreshButtonCustomID:
		m.layout.ResetStats(s)
		m.Show(event, s)
	case string(session.ViewerFirstPage), string(session.ViewerPrevPage), string(session.ViewerNextPage), string(session.ViewerLastPage):
		m.handlePagination(event, s, session.ViewerAction(customID))
	}
}

// handlePagination processes page navigation.
func (m *MainMenu) handlePagination(event *events.ComponentInteractionCreate, s *session.Session, action session.ViewerAction) {
	switch action {
	case session.ViewerNextPage:
		cursor := session.LeaderboardCursor.Get(s)
		nextCursor := session.LeaderboardNextCursor.Get(s)
		prevCursors := session.LeaderboardPrevCursors.Get(s)

		if session.PaginationHasNextPage.Get(s) {
			session.LeaderboardCursor.Set(s, nextCursor)
			session.LeaderboardPrevCursors.Set(s, append(prevCursors, cursor))
			m.Show(event, s)
		}
	case session.ViewerPrevPage:
		prevCursors := session.LeaderboardPrevCursors.Get(s)

		if len(prevCursors) > 0 {
			lastIdx := len(prevCursors) - 1
			session.LeaderboardPrevCursors.Set(s, prevCursors[:lastIdx])
			session.LeaderboardCursor.Set(s, prevCursors[lastIdx])
			m.Show(event, s)
		}
	case session.ViewerFirstPage:
		session.LeaderboardCursor.Set(s, nil)
		session.LeaderboardPrevCursors.Set(s, []*types.LeaderboardCursor{})
		m.Show(event, s)
	case session.ViewerLastPage:
		return
	}
}

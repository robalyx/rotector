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
	"github.com/robalyx/rotector/internal/bot/utils"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
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
	var settings *types.UserSetting
	s.GetInterface(constants.SessionKeyUserSettings, &settings)

	// Get cursor from session if it exists
	var cursor *types.LeaderboardCursor
	s.GetInterface(constants.SessionKeyLeaderboardCursor, &cursor)

	// Fetch leaderboard stats from database
	stats, nextCursor, err := m.layout.db.Votes().GetLeaderboard(
		context.Background(),
		settings.LeaderboardPeriod,
		cursor,
		constants.LeaderboardEntriesPerPage,
	)
	if err != nil {
		m.layout.logger.Error("Failed to get leaderboard stats", zap.Error(err))
		m.layout.paginationManager.NavigateTo(event, s, m.page, "Failed to retrieve leaderboard data. Please try again.")
		return
	}

	// Get refresh info for leaderboard view
	lastRefresh, nextRefresh, err := m.layout.db.Views().GetRefreshInfo(context.Background(), settings.LeaderboardPeriod)
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
	s.Set(constants.SessionKeyLeaderboardStats, stats)
	s.Set(constants.SessionKeyLeaderboardUsernames, usernames)
	s.Set(constants.SessionKeyLeaderboardNextCursor, nextCursor)
	s.Set(constants.SessionKeyHasNextPage, nextCursor != nil)
	s.Set(constants.SessionKeyHasPrevPage, cursor != nil)
	s.Set(constants.SessionKeyLeaderboardLastRefresh, lastRefresh)
	s.Set(constants.SessionKeyLeaderboardNextRefresh, nextRefresh)

	m.layout.paginationManager.NavigateTo(event, s, m.page, "")
}

// handleSelectMenu processes select menu interactions.
func (m *MainMenu) handleSelectMenu(event *events.ComponentInteractionCreate, s *session.Session, customID string, option string) {
	if customID != constants.LeaderboardPeriodSelectMenuCustomID {
		return
	}

	var settings *types.UserSetting
	s.GetInterface(constants.SessionKeyUserSettings, &settings)

	// Update user's leaderboard period preference
	settings.LeaderboardPeriod = types.LeaderboardPeriod(option)
	if err := m.layout.db.Settings().SaveUserSettings(context.Background(), settings); err != nil {
		m.layout.logger.Error("Failed to save user settings", zap.Error(err))
		m.layout.paginationManager.RespondWithError(event, "Failed to save time period preference. Please try again.")
		return
	}
	s.Set(constants.SessionKeyUserSettings, settings)

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
	case string(utils.ViewerFirstPage), string(utils.ViewerPrevPage), string(utils.ViewerNextPage), string(utils.ViewerLastPage):
		m.handlePagination(event, s, utils.ViewerAction(customID))
	}
}

// handlePagination processes page navigation.
func (m *MainMenu) handlePagination(event *events.ComponentInteractionCreate, s *session.Session, action utils.ViewerAction) {
	switch action {
	case utils.ViewerNextPage:
		var cursor *types.LeaderboardCursor
		s.GetInterface(constants.SessionKeyLeaderboardCursor, &cursor)
		var nextCursor *types.LeaderboardCursor
		s.GetInterface(constants.SessionKeyLeaderboardNextCursor, &nextCursor)
		var prevCursors []*types.LeaderboardCursor
		s.GetInterface(constants.SessionKeyLeaderboardPrevCursors, &prevCursors)

		if s.GetBool(constants.SessionKeyHasNextPage) {
			s.Set(constants.SessionKeyLeaderboardCursor, nextCursor)
			s.Set(constants.SessionKeyLeaderboardPrevCursors, append(prevCursors, cursor))
			m.Show(event, s)
		}
	case utils.ViewerPrevPage:
		var prevCursors []*types.LeaderboardCursor
		s.GetInterface(constants.SessionKeyLeaderboardPrevCursors, &prevCursors)

		if len(prevCursors) > 0 {
			lastIdx := len(prevCursors) - 1
			s.Set(constants.SessionKeyLeaderboardPrevCursors, prevCursors[:lastIdx])
			s.Set(constants.SessionKeyLeaderboardCursor, prevCursors[lastIdx])
			m.Show(event, s)
		}
	case utils.ViewerFirstPage:
		s.Set(constants.SessionKeyLeaderboardCursor, nil)
		s.Set(constants.SessionKeyLeaderboardPrevCursors, make([]*types.LeaderboardCursor, 0))
		m.Show(event, s)
	case utils.ViewerLastPage:
		return
	}
}

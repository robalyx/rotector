package guild

import (
	"context"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	builder "github.com/robalyx/rotector/internal/bot/builder/guild"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/pagination"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/interfaces"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
	"go.uber.org/zap"
)

// LogsMenu handles the display of guild ban operation logs.
type LogsMenu struct {
	layout *Layout
	page   *pagination.Page
}

// NewLogsMenu creates a new logs menu for viewing guild ban operations.
func NewLogsMenu(layout *Layout) *LogsMenu {
	m := &LogsMenu{layout: layout}
	m.page = &pagination.Page{
		Name: constants.GuildLogsPageName,
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return builder.NewLogsBuilder(s).Build()
		},
		ShowHandlerFunc:   m.Show,
		ButtonHandlerFunc: m.handleButton,
	}
	return m
}

// Show prepares and displays the guild ban logs interface.
func (m *LogsMenu) Show(event interfaces.CommonEvent, s *session.Session, r *pagination.Respond) {
	// Get guild ID from session
	guildID := session.GuildStatsID.Get(s)
	if guildID == 0 {
		r.Error(event, "Invalid guild ID.")
		return
	}

	// Set up activity filter for guild ban logs
	activityFilter := types.ActivityFilter{
		GuildID:      guildID,
		ActivityType: enum.ActivityTypeGuildBans,
	}

	// Get cursor from session if it exists
	cursor := session.LogCursor.Get(s)

	// Fetch filtered logs from database
	logs, nextCursor, err := m.layout.db.Models().Activities().GetLogs(
		context.Background(),
		activityFilter,
		cursor,
		constants.LogsPerPage,
	)
	if err != nil {
		m.layout.logger.Error("Failed to get guild ban logs", zap.Error(err))
		r.Error(event, "Failed to retrieve ban log data. Please try again.")
		return
	}

	// Get previous cursors array
	prevCursors := session.LogPrevCursors.Get(s)

	// Store results and cursor in session
	session.LogActivities.Set(s, logs)
	session.LogCursor.Set(s, cursor)
	session.LogNextCursor.Set(s, nextCursor)
	session.PaginationHasNextPage.Set(s, nextCursor != nil)
	session.PaginationHasPrevPage.Set(s, len(prevCursors) > 0)
}

// handleButton processes button interactions.
func (m *LogsMenu) handleButton(
	event *events.ComponentInteractionCreate, s *session.Session, r *pagination.Respond, customID string,
) {
	switch customID {
	case constants.BackButtonCustomID:
		r.NavigateBack(event, s, "")
	case constants.RefreshButtonCustomID:
		// Reset logs and reload
		session.LogCursor.Delete(s)
		session.LogNextCursor.Delete(s)
		session.LogPrevCursors.Delete(s)
		session.PaginationHasNextPage.Delete(s)
		session.PaginationHasPrevPage.Delete(s)
		r.Reload(event, s, "")
	case string(session.ViewerFirstPage),
		string(session.ViewerPrevPage),
		string(session.ViewerNextPage),
		string(session.ViewerLastPage):
		m.handlePagination(event, s, r, session.ViewerAction(customID))
	}
}

// handlePagination processes page navigation for logs.
func (m *LogsMenu) handlePagination(
	event *events.ComponentInteractionCreate, s *session.Session, r *pagination.Respond, action session.ViewerAction,
) {
	switch action {
	case session.ViewerNextPage:
		if session.PaginationHasNextPage.Get(s) {
			cursor := session.LogCursor.Get(s)
			nextCursor := session.LogNextCursor.Get(s)
			prevCursors := session.LogPrevCursors.Get(s)

			session.LogCursor.Set(s, nextCursor)
			session.LogPrevCursors.Set(s, append(prevCursors, cursor))
			r.Reload(event, s, "")
		}
	case session.ViewerPrevPage:
		prevCursors := session.LogPrevCursors.Get(s)

		if len(prevCursors) > 0 {
			lastIdx := len(prevCursors) - 1
			session.LogPrevCursors.Set(s, prevCursors[:lastIdx])
			session.LogCursor.Set(s, prevCursors[lastIdx])
			r.Reload(event, s, "")
		}
	case session.ViewerFirstPage:
		session.LogCursor.Set(s, nil)
		session.LogPrevCursors.Set(s, make([]*types.LogCursor, 0))
		r.Reload(event, s, "")
	case session.ViewerLastPage:
		// Not currently supported
		return
	}
}

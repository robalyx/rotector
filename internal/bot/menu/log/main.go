package log

import (
	"context"
	"fmt"
	"strconv"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	builder "github.com/robalyx/rotector/internal/bot/builder/log"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/pagination"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/interfaces"
	"github.com/robalyx/rotector/internal/bot/utils"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
	"go.uber.org/zap"
)

// MainMenu handles the display and interaction logic for viewing activity logs.
type MainMenu struct {
	layout *Layout
	page   *pagination.Page
}

// NewMainMenu creates a MainMenu and sets up its page with message builders and
// interaction handlers. The page is configured to show log entries
// and handle filtering/navigation.
func NewMainMenu(l *Layout) *MainMenu {
	m := &MainMenu{layout: l}
	m.page = &pagination.Page{
		Name: "Log Menu",
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return builder.NewBuilder(s).Build()
		},
		SelectHandlerFunc: m.handleSelectMenu,
		ButtonHandlerFunc: m.handleButton,
		ModalHandlerFunc:  m.handleModal,
	}
	return m
}

// Show prepares and displays the logs based on current filters
// and updates the session with the results.
func (m *MainMenu) Show(event interfaces.CommonEvent, s *session.Session) {
	// Get query parameters from session
	activityFilter := types.ActivityFilter{
		DiscordID:  s.GetUint64(constants.SessionKeyDiscordIDFilter),
		UserID:     s.GetUint64(constants.SessionKeyUserIDFilter),
		GroupID:    s.GetUint64(constants.SessionKeyGroupIDFilter),
		ReviewerID: s.GetUint64(constants.SessionKeyReviewerIDFilter),
		StartDate:  s.GetTime(constants.SessionKeyDateRangeStartFilter),
		EndDate:    s.GetTime(constants.SessionKeyDateRangeEndFilter),
	}
	s.GetInterface(constants.SessionKeyActivityTypeFilter, &activityFilter.ActivityType)

	// Get cursor from session if it exists
	var cursor *types.LogCursor
	s.GetInterface(constants.SessionKeyLogCursor, &cursor)

	// Fetch filtered logs from database
	logs, nextCursor, err := m.layout.db.Activity().GetLogs(
		context.Background(),
		activityFilter,
		cursor,
		constants.LogsPerPage,
	)
	if err != nil {
		m.layout.logger.Error("Failed to get logs", zap.Error(err))
		m.layout.paginationManager.NavigateTo(event, s, m.page, "Failed to retrieve log data. Please try again.")
		return
	}

	// If this is the first page (cursor is nil), create a cursor from the first log
	if cursor == nil && len(logs) > 0 {
		log := logs[0]
		cursor = &types.LogCursor{
			Timestamp: log.ActivityTimestamp,
			Sequence:  log.Sequence,
		}
	}

	// Get previous cursors array
	var prevCursors []*types.LogCursor
	s.GetInterface(constants.SessionKeyLogPrevCursors, &prevCursors)

	// Store results and cursor in session
	s.Set(constants.SessionKeyLogs, logs)
	s.Set(constants.SessionKeyLogCursor, cursor)
	s.Set(constants.SessionKeyLogNextCursor, nextCursor)
	s.Set(constants.SessionKeyHasNextPage, nextCursor != nil)
	s.Set(constants.SessionKeyHasPrevPage, len(prevCursors) > 0)

	m.layout.paginationManager.NavigateTo(event, s, m.page, "")
}

// handleSelectMenu processes select menu interactions by showing the appropriate
// query modal or updating the activity type filter.
func (m *MainMenu) handleSelectMenu(event *events.ComponentInteractionCreate, s *session.Session, customID string, option string) {
	switch customID {
	case constants.ActionSelectMenuCustomID:
		switch option {
		case constants.LogsQueryDiscordIDOption:
			m.showQueryModal(event, option, "Discord ID", "ID", "Enter the Discord ID to query logs")
		case constants.LogsQueryUserIDOption:
			m.showQueryModal(event, option, "User ID", "ID", "Enter the User ID to query logs")
		case constants.LogsQueryGroupIDOption:
			m.showQueryModal(event, option, "Group ID", "ID", "Enter the Group ID to query logs")
		case constants.LogsQueryReviewerIDOption:
			m.showQueryModal(event, option, "Reviewer ID", "ID", "Enter the Reviewer ID to query logs")
		case constants.LogsQueryDateRangeOption:
			m.showQueryModal(event, constants.LogsQueryDateRangeOption, "Date Range", "Date Range", "YYYY-MM-DD to YYYY-MM-DD")
		}

	case constants.LogsQueryActivityTypeFilterCustomID:
		// Convert activity type option to int and update filter
		optionInt, err := strconv.Atoi(option)
		if err != nil {
			m.layout.logger.Error("Failed to convert activity type option to int", zap.Error(err))
			m.layout.paginationManager.RespondWithError(event, "Invalid activity type option.")
			return
		}

		s.Set(constants.SessionKeyActivityTypeFilter, enum.ActivityType(optionInt))
		m.Show(event, s)
	}
}

// handleButton processes button interactions by handling navigation
// back to the dashboard and page navigation.
func (m *MainMenu) handleButton(event *events.ComponentInteractionCreate, s *session.Session, customID string) {
	switch customID {
	case constants.BackButtonCustomID:
		m.layout.paginationManager.NavigateBack(event, s, "")
	case constants.RefreshButtonCustomID:
		m.layout.ResetLogs(s)
		m.Show(event, s)
	case constants.ClearFiltersButtonCustomID:
		m.layout.ResetFilters(s)
		m.Show(event, s)
	case string(utils.ViewerFirstPage), string(utils.ViewerPrevPage), string(utils.ViewerNextPage), string(utils.ViewerLastPage):
		m.handlePagination(event, s, utils.ViewerAction(customID))
	}
}

// handleModal processes modal submissions by routing them to the appropriate
// handler based on the modal's custom ID.
func (m *MainMenu) handleModal(event *events.ModalSubmitInteractionCreate, s *session.Session) {
	customID := event.Data.CustomID
	switch customID {
	case constants.LogsQueryDiscordIDOption,
		constants.LogsQueryUserIDOption,
		constants.LogsQueryGroupIDOption,
		constants.LogsQueryReviewerIDOption:
		m.handleIDModalSubmit(event, s, customID)
	case constants.LogsQueryDateRangeOption:
		m.handleDateRangeModalSubmit(event, s)
	}
}

// showQueryModal creates and displays a modal for entering query parameters.
// The modal's fields are configured based on the type of query being performed.
func (m *MainMenu) showQueryModal(event *events.ComponentInteractionCreate, option, title, label, placeholder string) {
	modal := discord.NewModalCreateBuilder().
		SetCustomID(option).
		SetTitle(title).
		AddActionRow(
			discord.NewTextInput(constants.LogsQueryInputCustomID, discord.TextInputStyleShort, label).
				WithPlaceholder(placeholder).
				WithRequired(true),
		).
		Build()

	if err := event.Modal(modal); err != nil {
		m.layout.logger.Error("Failed to show query modal", zap.Error(err))
	}
}

// handleIDModalSubmit processes ID-based query modal submissions by parsing
// the ID and updating the appropriate session value.
func (m *MainMenu) handleIDModalSubmit(event *events.ModalSubmitInteractionCreate, s *session.Session, queryType string) {
	idStr := event.Data.Text(constants.LogsQueryInputCustomID)
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		m.layout.paginationManager.NavigateTo(event, s, m.page, "Invalid ID provided. Please enter a valid numeric ID.")
		return
	}

	// Store ID in appropriate session key based on query type
	switch queryType {
	case constants.LogsQueryDiscordIDOption:
		s.Set(constants.SessionKeyDiscordIDFilter, id)
	case constants.LogsQueryUserIDOption:
		s.Set(constants.SessionKeyUserIDFilter, id)
	case constants.LogsQueryGroupIDOption:
		s.Set(constants.SessionKeyGroupIDFilter, id)
	case constants.LogsQueryReviewerIDOption:
		s.Set(constants.SessionKeyReviewerIDFilter, id)
	}

	m.Show(event, s)
}

// handleDateRangeModalSubmit processes date range modal submissions by parsing
// the date range string and storing the dates in the session.
func (m *MainMenu) handleDateRangeModalSubmit(event *events.ModalSubmitInteractionCreate, s *session.Session) {
	dateRangeStr := event.Data.Text(constants.LogsQueryInputCustomID)
	startDate, endDate, err := utils.ParseDateRange(dateRangeStr)
	if err != nil {
		m.layout.paginationManager.NavigateTo(event, s, m.page, fmt.Sprintf("Invalid date range: %v", err))
		return
	}

	s.Set(constants.SessionKeyDateRangeStartFilter, startDate)
	s.Set(constants.SessionKeyDateRangeEndFilter, endDate)

	m.Show(event, s)
}

// handlePagination processes page navigation.
func (m *MainMenu) handlePagination(event *events.ComponentInteractionCreate, s *session.Session, action utils.ViewerAction) {
	switch action {
	case utils.ViewerNextPage:
		if s.GetBool(constants.SessionKeyHasNextPage) {
			var cursor *types.LogCursor
			s.GetInterface(constants.SessionKeyLogCursor, &cursor)
			var nextCursor *types.LogCursor
			s.GetInterface(constants.SessionKeyLogNextCursor, &nextCursor)
			var prevCursors []*types.LogCursor
			s.GetInterface(constants.SessionKeyLogPrevCursors, &prevCursors)

			s.Set(constants.SessionKeyLogCursor, nextCursor)
			s.Set(constants.SessionKeyLogPrevCursors, append(prevCursors, cursor))
			m.Show(event, s)
		}
	case utils.ViewerPrevPage:
		var prevCursors []*types.LogCursor
		s.GetInterface(constants.SessionKeyLogPrevCursors, &prevCursors)

		if len(prevCursors) > 0 {
			lastIdx := len(prevCursors) - 1
			s.Set(constants.SessionKeyLogPrevCursors, prevCursors[:lastIdx])
			s.Set(constants.SessionKeyLogCursor, prevCursors[lastIdx])
			m.Show(event, s)
		}
	case utils.ViewerFirstPage:
		s.Set(constants.SessionKeyLogCursor, nil)
		s.Set(constants.SessionKeyLogPrevCursors, make([]*types.LogCursor, 0))
		m.Show(event, s)
	case utils.ViewerLastPage:
		return
	}
}

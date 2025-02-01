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
		Name: constants.LogPageName,
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
		DiscordID:    session.LogFilterDiscordID.Get(s),
		UserID:       session.LogFilterUserID.Get(s),
		GroupID:      session.LogFilterGroupID.Get(s),
		ReviewerID:   session.LogFilterReviewerID.Get(s),
		ActivityType: session.LogFilterActivityType.Get(s),
		StartDate:    session.LogFilterDateRangeStart.Get(s),
		EndDate:      session.LogFilterDateRangeEnd.Get(s),
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
	prevCursors := session.LogPrevCursors.Get(s)

	// Store results and cursor in session
	session.LogActivities.Set(s, logs)
	session.LogCursor.Set(s, cursor)
	session.LogNextCursor.Set(s, nextCursor)
	session.PaginationHasNextPage.Set(s, nextCursor != nil)
	session.PaginationHasPrevPage.Set(s, len(prevCursors) > 0)

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
		// Handle category selection
		switch option {
		case constants.LogsUserActivityCategoryOption,
			constants.LogsGroupActivityCategoryOption,
			constants.LogsOtherActivityCategoryOption:
			session.LogFilterActivityCategory.Set(s, option)
			m.layout.paginationManager.NavigateTo(event, s, m.page, "")
			return
		}

		// Convert activity type option to int and update filter
		optionInt, err := strconv.Atoi(option)
		if err != nil {
			m.layout.logger.Error("Failed to convert activity type option to int", zap.Error(err))
			m.layout.paginationManager.RespondWithError(event, "Invalid activity type option.")
			return
		}

		session.LogFilterActivityType.Set(s, enum.ActivityType(optionInt))
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
	case string(session.ViewerFirstPage), string(session.ViewerPrevPage), string(session.ViewerNextPage), string(session.ViewerLastPage):
		m.handlePagination(event, s, session.ViewerAction(customID))
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
		session.LogFilterDiscordID.Set(s, id)
	case constants.LogsQueryUserIDOption:
		session.LogFilterUserID.Set(s, id)
	case constants.LogsQueryGroupIDOption:
		session.LogFilterGroupID.Set(s, id)
	case constants.LogsQueryReviewerIDOption:
		session.LogFilterReviewerID.Set(s, id)
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

	session.LogFilterDateRangeStart.Set(s, startDate)
	session.LogFilterDateRangeEnd.Set(s, endDate)

	m.Show(event, s)
}

// handlePagination processes page navigation.
func (m *MainMenu) handlePagination(event *events.ComponentInteractionCreate, s *session.Session, action session.ViewerAction) {
	switch action {
	case session.ViewerNextPage:
		if session.PaginationHasNextPage.Get(s) {
			cursor := session.LogCursor.Get(s)
			nextCursor := session.LogNextCursor.Get(s)
			prevCursors := session.LogPrevCursors.Get(s)

			session.LogCursor.Set(s, nextCursor)
			session.LogPrevCursors.Set(s, append(prevCursors, cursor))
			m.Show(event, s)
		}
	case session.ViewerPrevPage:
		prevCursors := session.LogPrevCursors.Get(s)

		if len(prevCursors) > 0 {
			lastIdx := len(prevCursors) - 1
			session.LogPrevCursors.Set(s, prevCursors[:lastIdx])
			session.LogCursor.Set(s, prevCursors[lastIdx])
			m.Show(event, s)
		}
	case session.ViewerFirstPage:
		session.LogCursor.Set(s, nil)
		session.LogPrevCursors.Set(s, make([]*types.LogCursor, 0))
		m.Show(event, s)
	case session.ViewerLastPage:
		return
	}
}

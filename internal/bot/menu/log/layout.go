package log

import (
	"context"
	"fmt"
	"strconv"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	builder "github.com/rotector/rotector/internal/bot/builder/log"
	"github.com/rotector/rotector/internal/bot/constants"
	"github.com/rotector/rotector/internal/bot/core/pagination"
	"github.com/rotector/rotector/internal/bot/core/session"
	"github.com/rotector/rotector/internal/bot/interfaces"
	"github.com/rotector/rotector/internal/bot/utils"
	"github.com/rotector/rotector/internal/common/storage/database/models"
	"go.uber.org/zap"
)

// Menu handles the display and interaction logic for viewing activity logs.
// It works with the log builder to create paginated views of user activity
// and provides filtering options.
type Menu struct {
	handler *Handler
	page    *pagination.Page
}

// NewMenu creates a Menu and sets up its page with message builders and
// interaction handlers. The page is configured to show log entries
// and handle filtering/navigation.
func NewMenu(h *Handler) *Menu {
	m := &Menu{handler: h}
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

// ShowLogMenu prepares and displays the logs based on current filters
// and updates the session with the results.
func (m *Menu) ShowLogMenu(event interfaces.CommonEvent, s *session.Session) {
	// Get query parameters from session
	activityFilter := models.ActivityFilter{
		UserID:     s.GetUint64(constants.SessionKeyUserIDFilter),
		GroupID:    s.GetUint64(constants.SessionKeyGroupIDFilter),
		ReviewerID: s.GetUint64(constants.SessionKeyReviewerIDFilter),
		StartDate:  s.GetTime(constants.SessionKeyDateRangeStartFilter),
		EndDate:    s.GetTime(constants.SessionKeyDateRangeEndFilter),
	}
	s.GetInterface(constants.SessionKeyActivityTypeFilter, &activityFilter.ActivityType)

	// Get cursor from session if it exists
	var cursor *models.LogCursor
	s.GetInterface(constants.SessionKeyCursor, &cursor)

	// Fetch filtered logs from database
	logs, nextCursor, err := m.handler.db.UserActivity().GetLogs(
		context.Background(),
		activityFilter,
		cursor,
		constants.LogsPerPage,
	)
	if err != nil {
		m.handler.logger.Error("Failed to get logs", zap.Error(err))
		m.handler.paginationManager.NavigateTo(event, s, m.page, "Failed to retrieve log data. Please try again.")
		return
	}

	// If this is the first page (cursor is nil), create a cursor from the first log
	if cursor == nil && len(logs) > 0 {
		cursor = &models.LogCursor{
			Timestamp: logs[0].ActivityTimestamp,
			Sequence:  logs[0].Sequence,
		}
	}

	// Get previous cursors array
	var prevCursors []*models.LogCursor
	s.GetInterface(constants.SessionKeyPrevCursors, &prevCursors)
	if prevCursors == nil {
		prevCursors = make([]*models.LogCursor, 0)
	}

	// Store results and cursor in session
	s.Set(constants.SessionKeyLogs, logs)
	s.Set(constants.SessionKeyCursor, cursor)
	s.Set(constants.SessionKeyNextCursor, nextCursor)
	s.Set(constants.SessionKeyHasNextPage, nextCursor != nil)
	s.Set(constants.SessionKeyHasPrevPage, len(prevCursors) > 0)

	m.handler.paginationManager.NavigateTo(event, s, m.page, "")
}

// handleSelectMenu processes select menu interactions by showing the appropriate
// query modal or updating the activity type filter.
func (m *Menu) handleSelectMenu(event *events.ComponentInteractionCreate, s *session.Session, customID string, option string) {
	switch customID {
	case constants.ActionSelectMenuCustomID:
		switch option {
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
			m.handler.logger.Error("Failed to convert activity type option to int", zap.Error(err))
			m.handler.paginationManager.RespondWithError(event, "Invalid activity type option.")
			return
		}

		s.Set(constants.SessionKeyActivityTypeFilter, models.ActivityType(optionInt))
		m.ShowLogMenu(event, s)
	}
}

// handleButton processes button interactions by handling navigation
// back to the dashboard and page navigation.
func (m *Menu) handleButton(event *events.ComponentInteractionCreate, s *session.Session, customID string) {
	switch customID {
	case constants.BackButtonCustomID:
		m.handler.paginationManager.NavigateBack(event, s, "")
	case constants.RefreshButtonCustomID:
		m.ShowLogMenu(event, s)
	case constants.ClearFiltersButtonCustomID:
		m.handler.ResetFilters(s)
		m.ShowLogMenu(event, s)
	case string(utils.ViewerFirstPage), string(utils.ViewerPrevPage), string(utils.ViewerNextPage), string(utils.ViewerLastPage):
		m.handlePagination(event, s, utils.ViewerAction(customID))
	}
}

// handleModal processes modal submissions by routing them to the appropriate
// handler based on the modal's custom ID.
func (m *Menu) handleModal(event *events.ModalSubmitInteractionCreate, s *session.Session) {
	customID := event.Data.CustomID
	switch customID {
	case constants.LogsQueryUserIDOption, constants.LogsQueryGroupIDOption, constants.LogsQueryReviewerIDOption:
		m.handleIDModalSubmit(event, s, customID)
	case constants.LogsQueryDateRangeOption:
		m.handleDateRangeModalSubmit(event, s)
	}
}

// showQueryModal creates and displays a modal for entering query parameters.
// The modal's fields are configured based on the type of query being performed.
func (m *Menu) showQueryModal(event *events.ComponentInteractionCreate, option, title, label, placeholder string) {
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
		m.handler.logger.Error("Failed to show query modal", zap.Error(err))
	}
}

// handleIDModalSubmit processes ID-based query modal submissions by parsing
// the ID and updating the appropriate session value.
func (m *Menu) handleIDModalSubmit(event *events.ModalSubmitInteractionCreate, s *session.Session, queryType string) {
	idStr := event.Data.Text(constants.LogsQueryInputCustomID)
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		m.handler.paginationManager.NavigateTo(event, s, m.page, "Invalid ID provided. Please enter a valid numeric ID.")
		return
	}

	// Store ID in appropriate session key based on query type
	switch queryType {
	case constants.LogsQueryUserIDOption:
		s.Set(constants.SessionKeyUserIDFilter, id)
	case constants.LogsQueryGroupIDOption:
		s.Set(constants.SessionKeyGroupIDFilter, id)
	case constants.LogsQueryReviewerIDOption:
		s.Set(constants.SessionKeyReviewerIDFilter, id)
	}

	m.ShowLogMenu(event, s)
}

// handleDateRangeModalSubmit processes date range modal submissions by parsing
// the date range string and storing the dates in the session.
func (m *Menu) handleDateRangeModalSubmit(event *events.ModalSubmitInteractionCreate, s *session.Session) {
	dateRangeStr := event.Data.Text(constants.LogsQueryInputCustomID)
	startDate, endDate, err := utils.ParseDateRange(dateRangeStr)
	if err != nil {
		m.handler.paginationManager.NavigateTo(event, s, m.page, fmt.Sprintf("Invalid date range: %v", err))
		return
	}

	s.Set(constants.SessionKeyDateRangeStartFilter, startDate)
	s.Set(constants.SessionKeyDateRangeEndFilter, endDate)

	m.ShowLogMenu(event, s)
}

// handlePagination processes page navigation.
func (m *Menu) handlePagination(event *events.ComponentInteractionCreate, s *session.Session, action utils.ViewerAction) {
	switch action {
	case utils.ViewerNextPage:
		if s.GetBool(constants.SessionKeyHasNextPage) {
			var cursor *models.LogCursor
			s.GetInterface(constants.SessionKeyCursor, &cursor)
			var nextCursor *models.LogCursor
			s.GetInterface(constants.SessionKeyNextCursor, &nextCursor)
			var prevCursors []*models.LogCursor
			s.GetInterface(constants.SessionKeyPrevCursors, &prevCursors)

			s.Set(constants.SessionKeyPrevCursors, append(prevCursors, cursor))
			s.Set(constants.SessionKeyCursor, nextCursor)
			m.ShowLogMenu(event, s)
		}
	case utils.ViewerPrevPage:
		var prevCursors []*models.LogCursor
		s.GetInterface(constants.SessionKeyPrevCursors, &prevCursors)
		if len(prevCursors) > 0 {
			lastIdx := len(prevCursors) - 1
			s.Set(constants.SessionKeyPrevCursors, prevCursors[:lastIdx])
			s.Set(constants.SessionKeyCursor, prevCursors[lastIdx])
			m.ShowLogMenu(event, s)
		}
	case utils.ViewerFirstPage:
		s.Set(constants.SessionKeyCursor, nil)
		s.Set(constants.SessionKeyPrevCursors, []*models.LogCursor{})
		m.ShowLogMenu(event, s)
	case utils.ViewerLastPage:
		m.handler.paginationManager.RespondWithError(event, "how are you here?")
	}
}

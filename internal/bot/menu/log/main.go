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

// Menu handles the display and interaction logic for viewing activity logs.
type Menu struct {
	layout *Layout
	page   *pagination.Page
}

// NewMenu creates a new log menu.
func NewMenu(l *Layout) *Menu {
	m := &Menu{layout: l}
	m.page = &pagination.Page{
		Name: constants.LogPageName,
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return builder.NewBuilder(s).Build()
		},
		ShowHandlerFunc:   m.Show,
		SelectHandlerFunc: m.handleSelectMenu,
		ButtonHandlerFunc: m.handleButton,
		ModalHandlerFunc:  m.handleModal,
	}
	return m
}

// Show prepares and displays the logs interface.
func (m *Menu) Show(event interfaces.CommonEvent, s *session.Session, r *pagination.Respond) {
	// Get query parameters from session
	activityFilter := types.ActivityFilter{
		GuildID:      session.LogFilterGuildID.Get(s),
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
	logs, nextCursor, err := m.layout.db.Model().Activity().GetLogs(
		context.Background(),
		activityFilter,
		cursor,
		constants.LogsPerPage,
	)
	if err != nil {
		m.layout.logger.Error("Failed to get logs", zap.Error(err))
		r.Error(event, "Failed to retrieve log data. Please try again.")
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

// handleSelectMenu processes select menu interactions.
func (m *Menu) handleSelectMenu(
	event *events.ComponentInteractionCreate, s *session.Session, r *pagination.Respond, customID, option string,
) {
	switch customID {
	case constants.ActionSelectMenuCustomID:
		switch option {
		case constants.LogsQueryGuildIDOption:
			m.showQueryModal(event, s, r, option, "Guild ID", "ID", "Enter the Guild ID to query logs")
		case constants.LogsQueryDiscordIDOption:
			m.showQueryModal(event, s, r, option, "Discord ID", "ID", "Enter the Discord ID to query logs")
		case constants.LogsQueryUserIDOption:
			m.showQueryModal(event, s, r, option, "User ID", "ID", "Enter the User ID to query logs")
		case constants.LogsQueryGroupIDOption:
			m.showQueryModal(event, s, r, option, "Group ID", "ID", "Enter the Group ID to query logs")
		case constants.LogsQueryReviewerIDOption:
			m.showQueryModal(event, s, r, option, "Reviewer ID", "ID", "Enter the Reviewer ID to query logs")
		case constants.LogsQueryDateRangeOption:
			m.showQueryModal(event, s, r, constants.LogsQueryDateRangeOption, "Date Range", "Date Range", "YYYY-MM-DD to YYYY-MM-DD")
		}

	case constants.LogsQueryActivityTypeFilterCustomID:
		// Handle category selection
		switch option {
		case constants.LogsUserActivityCategoryOption,
			constants.LogsGroupActivityCategoryOption,
			constants.LogsOtherActivityCategoryOption:
			session.LogFilterActivityCategory.Set(s, option)
			r.Reload(event, s, "")
			return
		}

		// Convert activity type option to int and update filter
		optionInt, err := strconv.Atoi(option)
		if err != nil {
			m.layout.logger.Error("Failed to convert activity type option to int", zap.Error(err))
			r.Error(event, "Invalid activity type option.")
			return
		}

		session.LogFilterActivityType.Set(s, enum.ActivityType(optionInt))
		r.Reload(event, s, "")
	}
}

// handleButton processes button interactions.
func (m *Menu) handleButton(
	event *events.ComponentInteractionCreate, s *session.Session, r *pagination.Respond, customID string,
) {
	switch customID {
	case constants.BackButtonCustomID:
		r.NavigateBack(event, s, "")
	case constants.RefreshButtonCustomID:
		ResetLogs(s)
		r.Reload(event, s, "")
	case constants.ClearFiltersButtonCustomID:
		ResetFilters(s)
		r.Reload(event, s, "")
	case string(session.ViewerFirstPage),
		string(session.ViewerPrevPage),
		string(session.ViewerNextPage),
		string(session.ViewerLastPage):
		m.handlePagination(event, s, r, session.ViewerAction(customID))
	}
}

// handleModal processes modal submissions by routing them to the appropriate
// handler based on the modal's custom ID.
func (m *Menu) handleModal(event *events.ModalSubmitInteractionCreate, s *session.Session, r *pagination.Respond) {
	customID := event.Data.CustomID
	switch customID {
	case constants.LogsQueryGuildIDOption,
		constants.LogsQueryDiscordIDOption,
		constants.LogsQueryUserIDOption,
		constants.LogsQueryGroupIDOption,
		constants.LogsQueryReviewerIDOption:
		m.handleIDModalSubmit(event, s, r, customID)
	case constants.LogsQueryDateRangeOption:
		m.handleDateRangeModalSubmit(event, s, r)
	}
}

// showQueryModal creates and displays a modal for entering query parameters.
func (m *Menu) showQueryModal(
	event *events.ComponentInteractionCreate, s *session.Session, r *pagination.Respond,
	option, title, label, placeholder string,
) {
	modal := discord.NewModalCreateBuilder().
		SetCustomID(option).
		SetTitle(title).
		AddActionRow(
			discord.NewTextInput(constants.LogsQueryInputCustomID, discord.TextInputStyleShort, label).
				WithPlaceholder(placeholder).
				WithRequired(true),
		)

	r.Modal(event, s, modal)
}

// handleIDModalSubmit processes ID-based query modal submissions.
func (m *Menu) handleIDModalSubmit(
	event *events.ModalSubmitInteractionCreate, s *session.Session, r *pagination.Respond, queryType string,
) {
	idStr := event.Data.Text(constants.LogsQueryInputCustomID)
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		r.Cancel(event, s, "Invalid ID provided. Please enter a valid numeric ID.")
		return
	}

	// Store ID in appropriate session key based on query type
	switch queryType {
	case constants.LogsQueryGuildIDOption:
		session.LogFilterGuildID.Set(s, id)
	case constants.LogsQueryDiscordIDOption:
		session.LogFilterDiscordID.Set(s, id)
	case constants.LogsQueryUserIDOption:
		session.LogFilterUserID.Set(s, id)
	case constants.LogsQueryGroupIDOption:
		session.LogFilterGroupID.Set(s, id)
	case constants.LogsQueryReviewerIDOption:
		session.LogFilterReviewerID.Set(s, id)
	}

	r.Reload(event, s, "")
}

// handleDateRangeModalSubmit processes date range modal submissions.
func (m *Menu) handleDateRangeModalSubmit(
	event *events.ModalSubmitInteractionCreate, s *session.Session, r *pagination.Respond,
) {
	dateRangeStr := event.Data.Text(constants.LogsQueryInputCustomID)
	startDate, endDate, err := utils.ParseDateRange(dateRangeStr)
	if err != nil {
		r.Cancel(event, s, fmt.Sprintf("Invalid date range: %v", err))
		return
	}

	session.LogFilterDateRangeStart.Set(s, startDate)
	session.LogFilterDateRangeEnd.Set(s, endDate)

	r.Reload(event, s, "")
}

// handlePagination processes page navigation.
func (m *Menu) handlePagination(
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
		return
	}
}

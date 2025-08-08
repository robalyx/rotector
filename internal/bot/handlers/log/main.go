package log

import (
	"fmt"
	"strconv"

	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/interaction"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/utils"
	view "github.com/robalyx/rotector/internal/bot/views/log"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/database/types/enum"
	"go.uber.org/zap"
)

// Menu handles the display and interaction logic for viewing activity logs.
type Menu struct {
	layout *Layout
	page   *interaction.Page
}

// NewMenu creates a new log menu.
func NewMenu(l *Layout) *Menu {
	m := &Menu{layout: l}
	m.page = &interaction.Page{
		Name: constants.LogPageName,
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return view.NewBuilder(s).Build()
		},
		ShowHandlerFunc:   m.Show,
		SelectHandlerFunc: m.handleSelectMenu,
		ButtonHandlerFunc: m.handleButton,
		ModalHandlerFunc:  m.handleModal,
	}

	return m
}

// Show prepares and displays the logs interface.
func (m *Menu) Show(ctx *interaction.Context, s *session.Session) {
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
		ctx.Context(),
		activityFilter,
		cursor,
		constants.LogsPerPage,
	)
	if err != nil {
		m.layout.logger.Error("Failed to get logs", zap.Error(err))
		ctx.Error("Failed to retrieve log data. Please try again.")

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
func (m *Menu) handleSelectMenu(ctx *interaction.Context, s *session.Session, customID, option string) {
	switch customID {
	case constants.ActionSelectMenuCustomID:
		switch option {
		case constants.LogsQueryGuildIDOption:
			m.showQueryModal(ctx, option, "Guild ID", "ID", "Enter the Guild ID to query logs")
		case constants.LogsQueryDiscordIDOption:
			m.showQueryModal(ctx, option, "Discord ID", "ID", "Enter the Discord ID to query logs")
		case constants.LogsQueryUserIDOption:
			m.showQueryModal(ctx, option, "User ID", "ID", "Enter the User ID to query logs")
		case constants.LogsQueryGroupIDOption:
			m.showQueryModal(ctx, option, "Group ID", "ID", "Enter the Group ID to query logs")
		case constants.LogsQueryReviewerIDOption:
			m.showQueryModal(ctx, option, "Reviewer ID", "ID", "Enter the Reviewer ID to query logs")
		case constants.LogsQueryDateRangeOption:
			m.showQueryModal(ctx, constants.LogsQueryDateRangeOption, "Date Range", "Date Range", "YYYY-MM-DD to YYYY-MM-DD")
		}

	case constants.LogsQueryActivityTypeFilterCustomID:
		// Handle category selection
		switch option {
		case constants.LogsUserActivityCategoryOption,
			constants.LogsGroupActivityCategoryOption,
			constants.LogsOtherActivityCategoryOption:
			session.LogFilterActivityCategory.Set(s, option)
			ctx.Reload("")

			return
		}

		// Convert activity type option to int and update filter
		optionInt, err := strconv.Atoi(option)
		if err != nil {
			m.layout.logger.Error("Failed to convert activity type option to int", zap.Error(err))
			ctx.Error("Invalid activity type option.")

			return
		}

		session.LogFilterActivityType.Set(s, enum.ActivityType(optionInt))
		ctx.Reload("")
	}
}

// handleButton processes button interactions.
func (m *Menu) handleButton(ctx *interaction.Context, s *session.Session, customID string) {
	switch customID {
	case constants.BackButtonCustomID:
		ctx.NavigateBack("")
	case constants.RefreshButtonCustomID:
		ResetLogs(s)
		ctx.Reload("")
	case constants.ClearFiltersButtonCustomID:
		ResetFilters(s)
		ctx.Reload("")
	case string(session.ViewerFirstPage),
		string(session.ViewerPrevPage),
		string(session.ViewerNextPage),
		string(session.ViewerLastPage):
		m.handlePagination(ctx, s, session.ViewerAction(customID))
	}
}

// handleModal processes modal submissions by routing them to the appropriate
// handler based on the modal's custom ID.
func (m *Menu) handleModal(ctx *interaction.Context, s *session.Session) {
	customID := ctx.Event().CustomID()
	switch customID {
	case constants.LogsQueryGuildIDOption,
		constants.LogsQueryDiscordIDOption,
		constants.LogsQueryUserIDOption,
		constants.LogsQueryGroupIDOption,
		constants.LogsQueryReviewerIDOption:
		m.handleIDModalSubmit(ctx, s, customID)
	case constants.LogsQueryDateRangeOption:
		m.handleDateRangeModalSubmit(ctx, s)
	}
}

// showQueryModal creates and displays a modal for entering query parameters.
func (m *Menu) showQueryModal(ctx *interaction.Context, option, title, label, placeholder string) {
	modal := discord.NewModalCreateBuilder().
		SetCustomID(option).
		SetTitle(title).
		AddActionRow(
			discord.NewTextInput(constants.LogsQueryInputCustomID, discord.TextInputStyleShort, label).
				WithPlaceholder(placeholder).
				WithRequired(true),
		)

	ctx.Modal(modal)
}

// handleIDModalSubmit processes ID-based query modal submissions.
func (m *Menu) handleIDModalSubmit(ctx *interaction.Context, s *session.Session, queryType string) {
	idStr := ctx.Event().ModalData().Text(constants.LogsQueryInputCustomID)

	// Store ID in appropriate session key based on query type
	switch queryType {
	case constants.LogsQueryGuildIDOption:
		id, err := strconv.ParseUint(idStr, 10, 64)
		if err != nil {
			ctx.Cancel("Invalid ID provided. Please enter a valid numeric ID.")
			return
		}

		session.LogFilterGuildID.Set(s, id)
	case constants.LogsQueryDiscordIDOption:
		id, err := strconv.ParseUint(idStr, 10, 64)
		if err != nil {
			ctx.Cancel("Invalid ID provided. Please enter a valid numeric ID.")
			return
		}

		session.LogFilterDiscordID.Set(s, id)
	case constants.LogsQueryUserIDOption:
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			ctx.Cancel("Invalid ID provided. Please enter a valid numeric ID.")
			return
		}

		if id <= 0 {
			ctx.Cancel("Invalid ID provided. Please enter a valid numeric ID.")
			return
		}

		session.LogFilterUserID.Set(s, id)
	case constants.LogsQueryGroupIDOption:
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			ctx.Cancel("Invalid ID provided. Please enter a valid numeric ID.")
			return
		}

		if id <= 0 {
			ctx.Cancel("Invalid ID provided. Please enter a valid numeric ID.")
			return
		}

		session.LogFilterGroupID.Set(s, id)
	case constants.LogsQueryReviewerIDOption:
		id, err := strconv.ParseUint(idStr, 10, 64)
		if err != nil {
			ctx.Cancel("Invalid ID provided. Please enter a valid numeric ID.")
			return
		}

		session.LogFilterReviewerID.Set(s, id)
	}

	ctx.Reload("")
}

// handleDateRangeModalSubmit processes date range modal submissions.
func (m *Menu) handleDateRangeModalSubmit(ctx *interaction.Context, s *session.Session) {
	dateRangeStr := ctx.Event().ModalData().Text(constants.LogsQueryInputCustomID)

	// Parse date range from string
	startDate, endDate, err := utils.ParseDateRange(dateRangeStr)
	if err != nil {
		ctx.Cancel(fmt.Sprintf("Invalid date range: %v", err))
		return
	}

	// Store date range in session
	session.LogFilterDateRangeStart.Set(s, startDate)
	session.LogFilterDateRangeEnd.Set(s, endDate)

	ctx.Reload("")
}

// handlePagination processes page navigation.
func (m *Menu) handlePagination(ctx *interaction.Context, s *session.Session, action session.ViewerAction) {
	switch action {
	case session.ViewerNextPage:
		if session.PaginationHasNextPage.Get(s) {
			cursor := session.LogCursor.Get(s)
			nextCursor := session.LogNextCursor.Get(s)
			prevCursors := session.LogPrevCursors.Get(s)

			session.LogCursor.Set(s, nextCursor)
			session.LogPrevCursors.Set(s, append(prevCursors, cursor))
			ctx.Reload("")
		}
	case session.ViewerPrevPage:
		prevCursors := session.LogPrevCursors.Get(s)

		if len(prevCursors) > 0 {
			lastIdx := len(prevCursors) - 1
			session.LogPrevCursors.Set(s, prevCursors[:lastIdx])
			session.LogCursor.Set(s, prevCursors[lastIdx])
			ctx.Reload("")
		}
	case session.ViewerFirstPage:
		session.LogCursor.Set(s, nil)
		session.LogPrevCursors.Set(s, make([]*types.LogCursor, 0))
		ctx.Reload("")
	case session.ViewerLastPage:
		return
	}
}

package appeal

import (
	"context"
	"errors"
	"strconv"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	builder "github.com/rotector/rotector/internal/bot/builder/appeal"
	"github.com/rotector/rotector/internal/bot/constants"
	"github.com/rotector/rotector/internal/bot/core/pagination"
	"github.com/rotector/rotector/internal/bot/core/session"
	"github.com/rotector/rotector/internal/bot/interfaces"
	"github.com/rotector/rotector/internal/bot/utils"
	"github.com/rotector/rotector/internal/common/storage/database/types"
	"go.uber.org/zap"
)

// OverviewMenu handles the display and interaction logic for the appeal overview.
type OverviewMenu struct {
	layout *Layout
	page   *pagination.Page
}

// NewOverviewMenu creates a new overview menu.
func NewOverviewMenu(layout *Layout) *OverviewMenu {
	m := &OverviewMenu{layout: layout}
	m.page = &pagination.Page{
		Name: "Appeal Overview",
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return builder.NewOverviewBuilder(s).Build()
		},
		SelectHandlerFunc: m.handleSelectMenu,
		ButtonHandlerFunc: m.handleButton,
		ModalHandlerFunc:  m.handleModal,
	}
	return m
}

// Show prepares and displays the appeal overview interface.
func (m *OverviewMenu) Show(event interfaces.CommonEvent, s *session.Session, content string) {
	// Get bot settings and user settings
	var settings *types.BotSetting
	s.GetInterface(constants.SessionKeyBotSettings, &settings)
	var userSettings *types.UserSetting
	s.GetInterface(constants.SessionKeyUserSettings, &userSettings)

	// Get cursor from session if it exists
	var cursor *types.AppealTimeline
	s.GetInterface(constants.SessionKeyAppealCursor, &cursor)

	// Get appeals based on user role and sort preference
	var appeals []*types.Appeal
	var firstCursor, nextCursor *types.AppealTimeline
	var err error

	userID := uint64(event.User().ID)
	if settings.IsReviewer(userID) {
		// Reviewers can see all appeals
		appeals, firstCursor, nextCursor, err = m.layout.db.Appeals().GetAppealsToReview(
			context.Background(),
			userSettings.AppealDefaultSort,
			userSettings.AppealStatusFilter,
			userID,
			cursor,
			constants.AppealsPerPage,
		)
	} else {
		// Regular users only see their own appeals
		appeals, firstCursor, nextCursor, err = m.layout.db.Appeals().GetAppealsByRequester(
			context.Background(),
			userSettings.AppealStatusFilter,
			userID,
			cursor,
			constants.AppealsPerPage,
		)
	}
	if err != nil {
		m.layout.logger.Error("Failed to get appeals", zap.Error(err))
		m.layout.paginationManager.RespondWithError(event, "Failed to get appeals. Please try again.")
		return
	}

	// Get previous cursors array
	var prevCursors []*types.AppealTimeline
	s.GetInterface(constants.SessionKeyAppealPrevCursors, &prevCursors)

	// Store data in session
	s.Set(constants.SessionKeyAppeals, appeals)
	s.Set(constants.SessionKeyAppealCursor, firstCursor)
	s.Set(constants.SessionKeyAppealNextCursor, nextCursor)
	s.Set(constants.SessionKeyHasNextPage, nextCursor != nil)
	s.Set(constants.SessionKeyHasPrevPage, len(prevCursors) > 0)

	m.layout.paginationManager.NavigateTo(event, s, m.page, content)
}

// handleSelectMenu processes select menu interactions.
func (m *OverviewMenu) handleSelectMenu(event *events.ComponentInteractionCreate, s *session.Session, customID string, option string) {
	switch customID {
	case constants.AppealStatusSelectID:
		// Retrieve user settings from session
		var settings *types.UserSetting
		s.GetInterface(constants.SessionKeyUserSettings, &settings)

		// Update user's default sort preference
		settings.AppealStatusFilter = types.AppealFilterBy(option)
		if err := m.layout.db.Settings().SaveUserSettings(context.Background(), settings); err != nil {
			m.layout.logger.Error("Failed to save user settings", zap.Error(err))
			m.layout.paginationManager.RespondWithError(event, "Failed to save sort order. Please try again.")
			return
		}
		s.Set(constants.SessionKeyUserSettings, settings)
		s.Delete(constants.SessionKeyAppealCursor)
		s.Delete(constants.SessionKeyAppealPrevCursors)

		m.Show(event, s, "Filtered appeals by "+option)
	case constants.AppealSortSelectID:
		// Retrieve user settings from session
		var settings *types.UserSetting
		s.GetInterface(constants.SessionKeyUserSettings, &settings)

		// Update user's default sort preference
		settings.AppealDefaultSort = types.AppealSortBy(option)
		if err := m.layout.db.Settings().SaveUserSettings(context.Background(), settings); err != nil {
			m.layout.logger.Error("Failed to save user settings", zap.Error(err))
			m.layout.paginationManager.RespondWithError(event, "Failed to save sort order. Please try again.")
			return
		}
		s.Set(constants.SessionKeyUserSettings, settings)
		s.Delete(constants.SessionKeyAppealCursor)
		s.Delete(constants.SessionKeyAppealPrevCursors)

		m.Show(event, s, "Sorted appeals by "+option)
	case constants.AppealSelectID:
		// Convert option string to int64 for appeal ID
		appealID, err := strconv.ParseInt(option, 10, 64)
		if err != nil {
			m.layout.logger.Error("Failed to parse appeal ID", zap.Error(err))
			m.layout.paginationManager.RespondWithError(event, "Invalid appeal ID format")
			return
		}
		// Show the selected appeal
		m.layout.ShowTicket(event, s, appealID, "")
	}
}

// handleButton processes button interactions.
func (m *OverviewMenu) handleButton(event *events.ComponentInteractionCreate, s *session.Session, customID string) {
	switch customID {
	case constants.BackButtonCustomID:
		m.layout.dashboardLayout.Show(event, s, "")
	case constants.AppealCreateButtonCustomID:
		m.handleCreateAppeal(event)
	case string(utils.ViewerFirstPage), string(utils.ViewerPrevPage), string(utils.ViewerNextPage), string(utils.ViewerLastPage):
		m.handlePagination(event, s, utils.ViewerAction(customID))
	}
}

// handleCreateAppeal opens a modal for creating a new appeal.
func (m *OverviewMenu) handleCreateAppeal(event *events.ComponentInteractionCreate) {
	modal := discord.NewModalCreateBuilder().
		SetCustomID(constants.AppealModalCustomID).
		SetTitle("Submit Appeal").
		AddActionRow(
			discord.NewTextInput(constants.AppealUserInputCustomID, discord.TextInputStyleShort, "User ID").
				WithRequired(true).
				WithPlaceholder("Enter the user ID to appeal..."),
		).
		AddActionRow(
			discord.NewTextInput(constants.AppealReasonInputCustomID, discord.TextInputStyleParagraph, "Appeal Reason").
				WithRequired(true).
				WithMaxLength(512).
				WithPlaceholder("Enter the reason for appealing this user..."),
		).
		Build()

	if err := event.Modal(modal); err != nil {
		m.layout.logger.Error("Failed to create appeal modal", zap.Error(err))
		m.layout.paginationManager.RespondWithError(event, "Failed to open the appeal modal. Please try again.")
	}
}

// handlePagination processes page navigation.
func (m *OverviewMenu) handlePagination(event *events.ComponentInteractionCreate, s *session.Session, action utils.ViewerAction) {
	switch action {
	case utils.ViewerNextPage:
		if s.GetBool(constants.SessionKeyHasNextPage) {
			var cursor *types.AppealTimeline
			s.GetInterface(constants.SessionKeyAppealCursor, &cursor)
			var nextCursor *types.AppealTimeline
			s.GetInterface(constants.SessionKeyAppealNextCursor, &nextCursor)
			var prevCursors []*types.AppealTimeline
			s.GetInterface(constants.SessionKeyAppealPrevCursors, &prevCursors)

			s.Set(constants.SessionKeyAppealCursor, nextCursor)
			s.Set(constants.SessionKeyAppealPrevCursors, append(prevCursors, cursor))
			m.Show(event, s, "")
		}
	case utils.ViewerPrevPage:
		var prevCursors []*types.AppealTimeline
		s.GetInterface(constants.SessionKeyAppealPrevCursors, &prevCursors)

		if len(prevCursors) > 0 {
			lastIdx := len(prevCursors) - 1
			s.Set(constants.SessionKeyAppealCursor, prevCursors[lastIdx])
			s.Set(constants.SessionKeyAppealPrevCursors, prevCursors[:lastIdx])
			m.Show(event, s, "")
		}
	case utils.ViewerFirstPage:
		s.Delete(constants.SessionKeyAppealCursor)
		s.Set(constants.SessionKeyAppealPrevCursors, make([]*types.AppealTimeline, 0))
		m.Show(event, s, "")
	case utils.ViewerLastPage:
		return
	}
}

// handleModal processes modal submissions.
func (m *OverviewMenu) handleModal(event *events.ModalSubmitInteractionCreate, s *session.Session) {
	switch event.Data.CustomID {
	case constants.AppealModalCustomID:
		m.handleCreateAppealModalSubmit(event, s)
	}
}

// handleCreateAppealModalSubmit processes the appeal creation form submission.
func (m *OverviewMenu) handleCreateAppealModalSubmit(event *events.ModalSubmitInteractionCreate, s *session.Session) {
	// Get and validate the user ID input
	userIDStr := event.Data.Text(constants.AppealUserInputCustomID)
	userID, err := strconv.ParseUint(userIDStr, 10, 64)
	if err != nil {
		m.layout.paginationManager.NavigateTo(event, s, m.page, "Invalid user ID format. Please enter a valid number.")
		return
	}

	// Check if the Discord user already has a pending appeal
	exists, err := m.layout.db.Appeals().HasPendingAppealByRequester(context.Background(), uint64(event.User().ID))
	if err != nil {
		m.layout.logger.Error("Failed to check pending appeals", zap.Error(err))
		m.layout.paginationManager.RespondWithError(event, "Failed to check pending appeals. Please try again.")
		return
	}
	if exists {
		m.layout.paginationManager.NavigateTo(event, s, m.page, "You already have a pending appeal. Please wait for it to be reviewed.")
		return
	}

	// Check if the user ID has been previously rejected
	hasRejection, err := m.layout.db.Appeals().HasPreviousRejection(context.Background(), userID)
	if err != nil {
		m.layout.logger.Error("Failed to check previous rejections", zap.Error(err))
		m.layout.paginationManager.RespondWithError(event, "Failed to check appeal history. Please try again.")
		return
	}
	if hasRejection {
		m.layout.paginationManager.NavigateTo(event, s, m.page,
			"This user ID has a previously rejected appeal. New appeals are not allowed.")
		return
	}

	// Verify user exists in database
	user, err := m.layout.db.Users().GetUserByID(context.Background(), userID, types.UserFields{Basic: true}, false)
	if err != nil {
		if errors.Is(err, types.ErrUserNotFound) {
			m.layout.paginationManager.NavigateTo(event, s, m.page, "Cannot submit appeal - user is not in our database.")
			return
		}
		m.layout.logger.Error("Failed to verify user status", zap.Error(err))
		m.layout.paginationManager.RespondWithError(event, "Failed to verify user status. Please try again.")
		return
	}

	// Only allow appeals for confirmed/flagged users
	if user.Status != types.UserTypeConfirmed && user.Status != types.UserTypeFlagged {
		m.layout.paginationManager.NavigateTo(event, s, m.page, "Cannot submit appeal - user must be confirmed or flagged.")
		return
	}

	// Get and validate the appeal reason
	reason := event.Data.Text(constants.AppealReasonInputCustomID)
	if reason == "" {
		m.layout.paginationManager.NavigateTo(event, s, m.page, "Appeal reason cannot be empty. Please try again.")
		return
	}

	// Show verification menu
	m.layout.ShowVerify(event, s, userID, reason)
}

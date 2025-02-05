package appeal

import (
	"context"
	"errors"
	"strconv"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	builder "github.com/robalyx/rotector/internal/bot/builder/appeal"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/pagination"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/interfaces"
	"github.com/robalyx/rotector/internal/bot/utils"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
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
		Name: constants.AppealOverviewPageName,
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return builder.NewOverviewBuilder(s).Build()
		},
		ShowHandlerFunc:   m.Show,
		SelectHandlerFunc: m.handleSelectMenu,
		ButtonHandlerFunc: m.handleButton,
		ModalHandlerFunc:  m.handleModal,
	}
	return m
}

// Show prepares and displays the appeal overview interface.
func (m *OverviewMenu) Show(event interfaces.CommonEvent, s *session.Session, r *pagination.Respond) {
	defaultSort := session.UserAppealDefaultSort.Get(s)
	statusFilter := session.UserAppealStatusFilter.Get(s)
	cursor := session.AppealCursor.Get(s)

	// Get appeals based on user role and sort preference
	var appeals []*types.Appeal
	var firstCursor, nextCursor *types.AppealTimeline
	var err error

	userID := uint64(event.User().ID)
	if s.BotSettings().IsReviewer(userID) {
		// Reviewers can see all appeals
		appeals, firstCursor, nextCursor, err = m.layout.db.Models().Appeals().GetAppealsToReview(
			context.Background(),
			defaultSort,
			statusFilter,
			userID,
			cursor,
			constants.AppealsPerPage,
		)
	} else {
		// Regular users only see their own appeals
		appeals, firstCursor, nextCursor, err = m.layout.db.Models().Appeals().GetAppealsByRequester(
			context.Background(),
			statusFilter,
			userID,
			cursor,
			constants.AppealsPerPage,
		)
	}
	if err != nil {
		m.layout.logger.Error("Failed to get appeals", zap.Error(err))
		r.Error(event, "Failed to get appeals. Please try again.")
		return
	}

	// Get previous cursors
	prevCursors := session.AppealPrevCursors.Get(s)

	// Store data in session
	session.AppealList.Set(s, appeals)
	session.AppealCursor.Set(s, firstCursor)
	session.AppealNextCursor.Set(s, nextCursor)
	session.PaginationHasNextPage.Set(s, nextCursor != nil)
	session.PaginationHasPrevPage.Set(s, len(prevCursors) > 0)
}

// handleSelectMenu processes select menu interactions.
func (m *OverviewMenu) handleSelectMenu(event *events.ComponentInteractionCreate, s *session.Session, r *pagination.Respond, customID, option string) {
	switch customID {
	case constants.AppealStatusSelectID:
		// Parse option to status
		status, err := enum.AppealStatusString(option)
		if err != nil {
			m.layout.logger.Error("Failed to parse status", zap.Error(err))
			r.Error(event, "Failed to parse sort order. Please try again.")
			return
		}

		// Update user's default sort preference
		session.UserAppealStatusFilter.Set(s, status)

		// Delete cursors
		session.AppealCursor.Delete(s)
		session.AppealPrevCursors.Delete(s)

		r.Reload(event, s, "Filtered appeals by "+status.String())
	case constants.AppealSortSelectID:
		// Parse option to appeal sort
		sortBy, err := enum.AppealSortByString(option)
		if err != nil {
			m.layout.logger.Error("Failed to parse sort order", zap.Error(err))
			r.Error(event, "Failed to parse sort order. Please try again.")
			return
		}

		// Update user's default sort preference
		session.UserAppealDefaultSort.Set(s, sortBy)

		// Delete cursors
		session.AppealCursor.Delete(s)
		session.AppealPrevCursors.Delete(s)

		r.Reload(event, s, "Sorted appeals by "+option)
	case constants.AppealSelectID:
		// Convert option string to int64 for appeal ID
		appealID, err := strconv.ParseInt(option, 10, 64)
		if err != nil {
			m.layout.logger.Error("Failed to parse appeal ID", zap.Error(err))
			r.Error(event, "Invalid appeal ID format")
			return
		}

		// Find the appeal in the session data
		appeals := session.AppealList.Get(s)

		var appeal *types.Appeal
		for _, a := range appeals {
			if a.ID == appealID {
				appeal = a
				break
			}
		}

		// Show error if appeal not found
		if appeal == nil {
			r.Error(event, "Appeal not found in current view")
			return
		}

		// Show the selected appeal
		session.AppealSelected.Set(s, appeal)
		r.Show(event, s, constants.AppealTicketPageName, "")
	}
}

// handleButton processes button interactions.
func (m *OverviewMenu) handleButton(event *events.ComponentInteractionCreate, s *session.Session, r *pagination.Respond, customID string) {
	switch customID {
	case constants.BackButtonCustomID:
		r.NavigateBack(event, s, "")
	case constants.RefreshButtonCustomID:
		session.AppealCursor.Delete(s)
		session.AppealPrevCursors.Delete(s)
		r.Reload(event, s, "Appeals refreshed.")
	case constants.AppealCreateButtonCustomID:
		m.handleCreateAppeal(event, r)
	case string(session.ViewerFirstPage), string(session.ViewerPrevPage), string(session.ViewerNextPage), string(session.ViewerLastPage):
		m.handlePagination(event, s, r, session.ViewerAction(customID))
	}
}

// handleCreateAppeal opens a modal for creating a new appeal.
func (m *OverviewMenu) handleCreateAppeal(event *events.ComponentInteractionCreate, r *pagination.Respond) {
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
		r.Error(event, "Failed to open the appeal modal. Please try again.")
	}
}

// handlePagination processes page navigation.
func (m *OverviewMenu) handlePagination(event *events.ComponentInteractionCreate, s *session.Session, r *pagination.Respond, action session.ViewerAction) {
	switch action {
	case session.ViewerNextPage:
		if session.PaginationHasNextPage.Get(s) {
			cursor := session.AppealCursor.Get(s)
			nextCursor := session.AppealNextCursor.Get(s)
			prevCursors := session.AppealPrevCursors.Get(s)

			session.AppealCursor.Set(s, nextCursor)
			session.AppealPrevCursors.Set(s, append(prevCursors, cursor))
			r.Reload(event, s, "")
		}
	case session.ViewerPrevPage:
		prevCursors := session.AppealPrevCursors.Get(s)

		if len(prevCursors) > 0 {
			lastIdx := len(prevCursors) - 1
			session.AppealCursor.Set(s, prevCursors[lastIdx])
			session.AppealPrevCursors.Set(s, prevCursors[:lastIdx])
			r.Reload(event, s, "")
		}
	case session.ViewerFirstPage:
		session.AppealCursor.Delete(s)
		session.AppealPrevCursors.Set(s, make([]*types.AppealTimeline, 0))
		r.Reload(event, s, "")
	case session.ViewerLastPage:
		return
	}
}

// handleModal processes modal submissions.
func (m *OverviewMenu) handleModal(event *events.ModalSubmitInteractionCreate, s *session.Session, r *pagination.Respond) {
	switch event.Data.CustomID {
	case constants.AppealModalCustomID:
		m.handleCreateAppealModalSubmit(event, s, r)
	}
}

// handleCreateAppealModalSubmit processes the appeal creation form submission.
func (m *OverviewMenu) handleCreateAppealModalSubmit(event *events.ModalSubmitInteractionCreate, s *session.Session, r *pagination.Respond) {
	// Get user ID input
	userIDStr := event.Data.Text(constants.AppealUserInputCustomID)

	// Parse profile URL if provided
	if utils.IsRobloxProfileURL(userIDStr) {
		var err error
		userIDStr, err = utils.ExtractUserIDFromURL(userIDStr)
		if err != nil {
			r.Cancel(event, s, "Invalid Roblox profile URL. Please provide a valid URL or ID.")
			return
		}
	}

	// Parse the user ID
	userID, err := strconv.ParseUint(userIDStr, 10, 64)
	if err != nil {
		r.Cancel(event, s, "Invalid user ID format. Please enter a valid number.")
		return
	}

	// Check if the user ID already has a pending appeal
	exists, err := m.layout.db.Models().Appeals().HasPendingAppealByUserID(context.Background(), userID)
	if err != nil {
		m.layout.logger.Error("Failed to check pending appeals for user", zap.Error(err))
		r.Error(event, "Failed to check pending appeals. Please try again.")
		return
	}
	if exists {
		r.Cancel(event, s, "This user ID already has a pending appeal. Please wait for it to be reviewed.")
		return
	}

	// Check if the Discord user already has a pending appeal
	exists, err = m.layout.db.Models().Appeals().HasPendingAppealByRequester(context.Background(), uint64(event.User().ID))
	if err != nil {
		m.layout.logger.Error("Failed to check pending appeals", zap.Error(err))
		r.Error(event, "Failed to check pending appeals. Please try again.")
		return
	}
	if exists {
		r.Cancel(event, s, "You already have a pending appeal. Please wait for it to be reviewed.")
		return
	}

	// Check if the user ID has been previously rejected
	hasRejection, err := m.layout.db.Models().Appeals().HasPreviousRejection(context.Background(), userID)
	if err != nil {
		m.layout.logger.Error("Failed to check previous rejections", zap.Error(err))
		r.Error(event, "Failed to check appeal history. Please try again.")
		return
	}
	if hasRejection {
		r.Cancel(event, s, "This user ID has a rejected appeal in the last 7 days. Please wait before submitting a new appeal.")
		return
	}

	// Verify user exists in database
	user, err := m.layout.db.Models().Users().GetUserByID(context.Background(), userIDStr, types.UserFieldAll)
	if err != nil {
		if errors.Is(err, types.ErrUserNotFound) {
			r.Cancel(event, s, "Cannot submit appeal - user is not in our database.")
			return
		}
		m.layout.logger.Error("Failed to verify user status", zap.Error(err))
		r.Error(event, "Failed to verify user status. Please try again.")
		return
	}

	// Only allow appeals for confirmed/flagged users
	if user.Status != enum.UserTypeConfirmed && user.Status != enum.UserTypeFlagged {
		r.Cancel(event, s, "Cannot submit appeal - user must be confirmed or flagged.")
		return
	}

	// Get and validate the appeal reason
	reason := event.Data.Text(constants.AppealReasonInputCustomID)
	if reason == "" {
		r.Cancel(event, s, "Appeal reason cannot be empty. Please try again.")
		return
	}

	// Show verification menu
	session.VerifyUserID.Set(s, userID)
	session.VerifyReason.Set(s, reason)
	r.Show(event, s, constants.AppealVerifyPageName, "")
}

package appeal

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"

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
	// If the appeal list is already set, don't fetch again
	if session.AppealList.Get(s) != nil {
		return
	}

	defaultSort := session.UserAppealDefaultSort.Get(s)
	statusFilter := session.UserAppealStatusFilter.Get(s)
	cursor := session.AppealCursor.Get(s)

	// Get appeals based on user role and sort preference
	var appeals []*types.FullAppeal
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
func (m *OverviewMenu) handleSelectMenu(
	event *events.ComponentInteractionCreate, s *session.Session, r *pagination.Respond, customID, option string,
) {
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
		ResetAppealData(s)

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
		ResetAppealData(s)

		r.Reload(event, s, "Sorted appeals by "+option)
	case constants.AppealSelectID:
		// Check if this is a modal open request
		if option == constants.AppealSearchCustomID {
			modal := discord.NewModalCreateBuilder().
				SetCustomID(constants.AppealSearchModalCustomID).
				SetTitle("Search Appeal").
				AddActionRow(
					discord.NewTextInput(constants.AppealIDInputCustomID, discord.TextInputStyleShort, "Appeal ID").
						WithRequired(true).
						WithPlaceholder("Enter the appeal ID..."),
				)
			r.Modal(event, s, modal)
			return
		}

		// Convert option string to int64 for appeal ID
		appealID, err := strconv.ParseInt(option, 10, 64)
		if err != nil {
			m.layout.logger.Error("Failed to parse appeal ID", zap.Error(err))
			r.Error(event, "Invalid appeal ID format")
			return
		}

		// Find the appeal in the session data
		appeals := session.AppealList.Get(s)

		var appeal *types.FullAppeal
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
	case constants.AppealCreateSelectID:
		switch option {
		case constants.AppealCreateRobloxButtonCustomID:
			session.AppealType.Set(s, enum.AppealTypeRoblox)
			m.handleCreateRobloxAppeal(event, s, r)
		case constants.AppealCreateDiscordButtonCustomID:
			session.AppealType.Set(s, enum.AppealTypeDiscord)
			m.handleCreateDiscordAppeal(event, s, r)
		}
	}
}

// handleButton processes button interactions.
func (m *OverviewMenu) handleButton(
	event *events.ComponentInteractionCreate, s *session.Session, r *pagination.Respond, customID string,
) {
	switch customID {
	case constants.BackButtonCustomID:
		r.NavigateBack(event, s, "")
	case constants.RefreshButtonCustomID:
		ResetAppealData(s)
		r.Reload(event, s, "Appeals refreshed.")
	case string(session.ViewerFirstPage),
		string(session.ViewerPrevPage),
		string(session.ViewerNextPage),
		string(session.ViewerLastPage):
		m.handlePagination(event, s, r, session.ViewerAction(customID))
	}
}

// handlePagination processes page navigation.
func (m *OverviewMenu) handlePagination(
	event *events.ComponentInteractionCreate, s *session.Session, r *pagination.Respond, action session.ViewerAction,
) {
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
func (m *OverviewMenu) handleModal(
	event *events.ModalSubmitInteractionCreate, s *session.Session, r *pagination.Respond,
) {
	switch event.Data.CustomID {
	case constants.AppealModalCustomID:
		m.handleCreateAppealModalSubmit(event, s, r)
	case constants.AppealSearchModalCustomID:
		m.handleSearchAppealModalSubmit(event, s, r)
	}
}

// handleCreateRobloxAppeal opens a modal for creating a new Roblox user appeal.
func (m *OverviewMenu) handleCreateRobloxAppeal(
	event *events.ComponentInteractionCreate, s *session.Session, r *pagination.Respond,
) {
	modal := discord.NewModalCreateBuilder().
		SetCustomID(constants.AppealModalCustomID).
		SetTitle("Submit Roblox User Appeal").
		AddActionRow(
			discord.NewTextInput(constants.AppealUserInputCustomID, discord.TextInputStyleShort, "Roblox User ID").
				WithRequired(true).
				WithPlaceholder("Enter the Roblox user ID to appeal..."),
		).
		AddActionRow(
			discord.NewTextInput(constants.AppealReasonInputCustomID, discord.TextInputStyleParagraph, "Appeal Reason").
				WithRequired(true).
				WithMinLength(128).
				WithMaxLength(512).
				WithPlaceholder("Enter the reason for appealing this user..."),
		)

	r.Modal(event, s, modal)
}

// handleCreateDiscordAppeal opens a modal for creating a new Discord user appeal.
func (m *OverviewMenu) handleCreateDiscordAppeal(
	event *events.ComponentInteractionCreate, s *session.Session, r *pagination.Respond,
) {
	modal := discord.NewModalCreateBuilder().
		SetCustomID(constants.AppealModalCustomID).
		SetTitle("Submit Discord User Appeal").
		AddActionRow(
			discord.NewTextInput(constants.AppealReasonInputCustomID, discord.TextInputStyleParagraph, "Appeal Reason").
				WithRequired(true).
				WithMinLength(128).
				WithMaxLength(512).
				WithPlaceholder("Enter the reason for appealing your Discord account..."),
		)

	r.Modal(event, s, modal)
}

// handleCreateAppealModalSubmit processes the appeal creation form submission.
func (m *OverviewMenu) handleCreateAppealModalSubmit(
	event *events.ModalSubmitInteractionCreate, s *session.Session, r *pagination.Respond,
) {
	// Get reason input
	reason := event.Data.Text(constants.AppealReasonInputCustomID)

	// Check if reason is empty
	if reason == "" {
		r.Cancel(event, s, "Appeal reason cannot be empty.")
		return
	}

	// Route to appropriate handler based on appeal type
	appealType := session.AppealType.Get(s)
	switch appealType {
	case enum.AppealTypeRoblox:
		userIDStr := event.Data.Text(constants.AppealUserInputCustomID)
		m.handleRobloxAppealSubmit(event, s, r, userIDStr, reason)
	case enum.AppealTypeDiscord:
		m.handleDiscordAppealSubmit(event, s, r, reason)
	default:
		r.Error(event, "Invalid appeal type. Please try again.")
	}
}

// handleRobloxAppealSubmit processes a Roblox user ID appeal submission.
func (m *OverviewMenu) handleRobloxAppealSubmit(
	event *events.ModalSubmitInteractionCreate, s *session.Session, r *pagination.Respond,
	userIDStr string, reason string,
) {
	// Parse profile URL if provided
	parsedURL, err := utils.ExtractUserIDFromURL(userIDStr)
	if err == nil {
		userIDStr = parsedURL
	}

	// Parse the user ID
	userID, err := strconv.ParseUint(userIDStr, 10, 64)
	if err != nil {
		r.Cancel(event, s, "Invalid Roblox user ID format. Please enter a valid number.")
		return
	}

	ctx := context.Background()

	// Check if the user ID already has a pending appeal
	exists, err := m.layout.db.Models().Appeals().HasPendingAppealByUserID(ctx, userID, enum.AppealTypeRoblox)
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
	exists, err = m.layout.db.Models().Appeals().HasPendingAppealByRequester(
		ctx, uint64(event.User().ID), enum.AppealTypeRoblox,
	)
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
	hasRejection, err := m.layout.db.Models().Appeals().HasPreviousRejection(ctx, userID, enum.AppealTypeRoblox)
	if err != nil {
		m.layout.logger.Error("Failed to check previous rejections", zap.Error(err))
		r.Error(event, "Failed to check appeal history. Please try again.")
		return
	}
	if hasRejection {
		r.Cancel(event, s, "This user ID has a rejected appeal recently. Please wait at least 7 days.")
		return
	}

	// Verify user exists in database
	user, err := m.layout.db.Models().Users().GetUserByID(ctx, userIDStr, types.UserFieldAll)
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

	// Show verification menu
	session.VerifyUserID.Set(s, userID)
	session.VerifyReason.Set(s, reason)
	r.Show(event, s, constants.AppealVerifyPageName, "")
}

// handleDiscordAppealSubmit processes a Discord user ID appeal submission.
func (m *OverviewMenu) handleDiscordAppealSubmit(
	event *events.ModalSubmitInteractionCreate, s *session.Session, r *pagination.Respond, reason string,
) {
	userID := uint64(event.User().ID)
	ctx := context.Background()

	// Check if user has any flags in the system
	totalGuilds, err := m.layout.db.Models().Sync().GetDiscordUserGuildCount(ctx, userID)
	if err != nil {
		m.layout.logger.Error("Failed to get Discord user guild count", zap.Error(err))
		r.Error(event, "Failed to verify Discord user status. Please try again.")
		return
	}

	messageSummary, err := m.layout.db.Models().Message().GetUserInappropriateMessageSummary(ctx, userID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		m.layout.logger.Error("Failed to get message summary", zap.Error(err))
		r.Error(event, "Failed to verify Discord user status. Please try again.")
		return
	}

	// Check if user is actually flagged
	if totalGuilds == 0 && (messageSummary == nil || messageSummary.MessageCount == 0) {
		r.Cancel(event, s, "Your Discord account is not flagged in our system.")
		return
	}

	// Check for existing pending appeals
	exists, err := m.layout.db.Models().Appeals().HasPendingAppealByUserID(ctx, userID, enum.AppealTypeDiscord)
	if err != nil {
		m.layout.logger.Error("Failed to check pending appeals", zap.Error(err))
		r.Error(event, "Failed to check pending appeals. Please try again.")
		return
	}
	if exists {
		r.Cancel(event, s, "You already have a pending appeal. Please wait for it to be reviewed.")
		return
	}

	// Check for previous rejections
	hasRejection, err := m.layout.db.Models().Appeals().HasPreviousRejection(ctx, userID, enum.AppealTypeDiscord)
	if err != nil {
		m.layout.logger.Error("Failed to check previous rejections", zap.Error(err))
		r.Error(event, "Failed to check appeal history. Please try again.")
		return
	}
	if hasRejection {
		r.Cancel(event, s, "You have a rejected appeal recently. Please wait at least 7 days.")
		return
	}

	// Create the appeal
	appeal := &types.Appeal{
		UserID:      userID,
		RequesterID: userID, // Same as UserID for Discord appeals
		Status:      enum.AppealStatusPending,
		Type:        enum.AppealTypeDiscord,
		Timestamp:   time.Now(),
	}

	// Submit appeal
	if err := m.layout.db.Models().Appeals().CreateAppeal(ctx, appeal, reason); err != nil {
		m.layout.logger.Error("Failed to create appeal", zap.Error(err))
		r.Error(event, "Failed to submit appeal. Please try again.")
		return
	}

	session.AppealCursor.Delete(s)
	session.AppealPrevCursors.Delete(s)
	r.Show(event, s, constants.AppealOverviewPageName, "✅ Appeal submitted successfully!")

	// Log the appeal submission
	m.layout.db.Models().Activities().Log(ctx, &types.ActivityLog{
		ActivityTarget: types.ActivityTarget{
			UserID: userID,
		},
		ReviewerID:        userID,
		ActivityType:      enum.ActivityTypeAppealSubmitted,
		ActivityTimestamp: time.Now(),
		Details: map[string]any{
			"reason": reason,
			"type":   enum.AppealTypeDiscord.String(),
		},
	})
}

// handleSearchAppealModalSubmit processes the appeal search form submission.
func (m *OverviewMenu) handleSearchAppealModalSubmit(
	event *events.ModalSubmitInteractionCreate, s *session.Session, r *pagination.Respond,
) {
	// Verify user is a reviewer
	if !s.BotSettings().IsReviewer(uint64(event.User().ID)) {
		r.Cancel(event, s, "Only reviewers can search appeals by ID.")
		return
	}

	// Get appeal ID input
	appealIDStr := event.Data.Text(constants.AppealIDInputCustomID)
	appealID, err := strconv.ParseInt(appealIDStr, 10, 64)
	if err != nil {
		r.Cancel(event, s, "Invalid appeal ID format. Please enter a valid number.")
		return
	}

	// Get the appeal
	appeal, err := m.layout.db.Models().Appeals().GetAppealByID(context.Background(), appealID)
	if err != nil {
		if errors.Is(err, types.ErrNoAppealsFound) {
			r.Cancel(event, s, "Appeal not found.")
			return
		}
		m.layout.logger.Error("Failed to get appeal", zap.Error(err))
		r.Error(event, "Failed to get appeal. Please try again.")
		return
	}

	// Show the selected appeal
	session.AppealSelected.Set(s, appeal)
	r.Show(event, s, constants.AppealTicketPageName, "")
}

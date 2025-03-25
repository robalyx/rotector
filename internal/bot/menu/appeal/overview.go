package appeal

import (
	"database/sql"
	"errors"
	"strconv"
	"time"

	"github.com/disgoorg/disgo/discord"
	builder "github.com/robalyx/rotector/internal/bot/builder/appeal"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/interaction"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
	"github.com/robalyx/rotector/internal/common/utils"
	"go.uber.org/zap"
)

// OverviewMenu handles the display and interaction logic for the appeal overview.
type OverviewMenu struct {
	layout *Layout
	page   *interaction.Page
}

// NewOverviewMenu creates a new overview menu.
func NewOverviewMenu(layout *Layout) *OverviewMenu {
	m := &OverviewMenu{layout: layout}
	m.page = &interaction.Page{
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
func (m *OverviewMenu) Show(ctx *interaction.Context, s *session.Session) {
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

	userID := uint64(ctx.Event().User().ID)
	if s.BotSettings().IsReviewer(userID) {
		// Reviewers can see all appeals
		appeals, firstCursor, nextCursor, err = m.layout.db.Service().Appeal().GetAppealsToReview(
			ctx.Context(),
			defaultSort,
			statusFilter,
			userID,
			cursor,
			constants.AppealsPerPage,
		)
	} else {
		// Regular users only see their own appeals
		appeals, firstCursor, nextCursor, err = m.layout.db.Service().Appeal().GetAppealsByRequester(
			ctx.Context(),
			statusFilter,
			userID,
			cursor,
			constants.AppealsPerPage,
		)
	}
	if err != nil {
		m.layout.logger.Error("Failed to get appeals", zap.Error(err))
		ctx.Error("Failed to get appeals. Please try again.")
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
func (m *OverviewMenu) handleSelectMenu(ctx *interaction.Context, s *session.Session, customID, option string) {
	switch customID {
	case constants.AppealStatusSelectID:
		// Parse option to status
		status, err := enum.AppealStatusString(option)
		if err != nil {
			m.layout.logger.Error("Failed to parse status", zap.Error(err))
			ctx.Error("Failed to parse sort order. Please try again.")
			return
		}

		// Update user's default sort preference
		session.UserAppealStatusFilter.Set(s, status)
		ResetAppealData(s)

		ctx.Reload("Filtered appeals by " + status.String())
	case constants.AppealSortSelectID:
		// Parse option to appeal sort
		sortBy, err := enum.AppealSortByString(option)
		if err != nil {
			m.layout.logger.Error("Failed to parse sort order", zap.Error(err))
			ctx.Error("Failed to parse sort order. Please try again.")
			return
		}

		// Update user's default sort preference
		session.UserAppealDefaultSort.Set(s, sortBy)
		ResetAppealData(s)

		ctx.Reload("Sorted appeals by " + option)
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
			ctx.Modal(modal)
			return
		}

		// Convert option string to int64 for appeal ID
		appealID, err := strconv.ParseInt(option, 10, 64)
		if err != nil {
			m.layout.logger.Error("Failed to parse appeal ID", zap.Error(err))
			ctx.Error("Invalid appeal ID format")
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
			ctx.Error("Appeal not found in current view")
			return
		}

		// Show the selected appeal
		session.PaginationPage.Set(s, 0)
		session.AppealSelected.Set(s, appeal)
		ctx.Show(constants.AppealTicketPageName, "")
	case constants.AppealCreateSelectID:
		switch option {
		case constants.AppealCreateRobloxButtonCustomID:
			session.AppealType.Set(s, enum.AppealTypeRoblox)
			m.handleCreateRobloxAppeal(ctx)
		case constants.AppealCreateDiscordButtonCustomID:
			session.AppealType.Set(s, enum.AppealTypeDiscord)
			m.handleCreateDiscordAppeal(ctx)
		}
	}
}

// handleButton processes button interactions.
func (m *OverviewMenu) handleButton(ctx *interaction.Context, s *session.Session, customID string) {
	switch customID {
	case constants.BackButtonCustomID:
		ctx.NavigateBack("")
	case constants.RefreshButtonCustomID:
		ResetAppealData(s)
		ctx.Reload("Appeals refreshed.")
	case string(session.ViewerFirstPage),
		string(session.ViewerPrevPage),
		string(session.ViewerNextPage),
		string(session.ViewerLastPage):
		m.handlePagination(ctx, s, session.ViewerAction(customID))
	}
}

// handlePagination processes page navigation.
func (m *OverviewMenu) handlePagination(ctx *interaction.Context, s *session.Session, action session.ViewerAction) {
	switch action {
	case session.ViewerNextPage:
		if session.PaginationHasNextPage.Get(s) {
			cursor := session.AppealCursor.Get(s)
			nextCursor := session.AppealNextCursor.Get(s)
			prevCursors := session.AppealPrevCursors.Get(s)

			session.AppealCursor.Set(s, nextCursor)
			session.AppealPrevCursors.Set(s, append(prevCursors, cursor))
			ctx.Reload("")
		}
	case session.ViewerPrevPage:
		prevCursors := session.AppealPrevCursors.Get(s)

		if len(prevCursors) > 0 {
			lastIdx := len(prevCursors) - 1
			session.AppealCursor.Set(s, prevCursors[lastIdx])
			session.AppealPrevCursors.Set(s, prevCursors[:lastIdx])
			ctx.Reload("")
		}
	case session.ViewerFirstPage:
		session.AppealCursor.Delete(s)
		session.AppealPrevCursors.Set(s, make([]*types.AppealTimeline, 0))
		ctx.Reload("")
	case session.ViewerLastPage:
		return
	}
}

// handleModal processes modal submissions.
func (m *OverviewMenu) handleModal(ctx *interaction.Context, s *session.Session) {
	switch ctx.Event().CustomID() {
	case constants.AppealModalCustomID:
		m.handleCreateAppealModalSubmit(ctx, s)
	case constants.AppealSearchModalCustomID:
		m.handleSearchAppealModalSubmit(ctx, s)
	}
}

// handleCreateRobloxAppeal opens a modal for creating a new Roblox user appeal.
func (m *OverviewMenu) handleCreateRobloxAppeal(ctx *interaction.Context) {
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

	ctx.Modal(modal)
}

// handleCreateDiscordAppeal opens a modal for creating a new Discord user appeal.
func (m *OverviewMenu) handleCreateDiscordAppeal(ctx *interaction.Context) {
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

	ctx.Modal(modal)
}

// handleCreateAppealModalSubmit processes the appeal creation form submission.
func (m *OverviewMenu) handleCreateAppealModalSubmit(ctx *interaction.Context, s *session.Session) {
	modalData := ctx.Event().ModalData()

	// Get reason input
	reason := modalData.Text(constants.AppealReasonInputCustomID)

	// Check if reason is empty
	if reason == "" {
		ctx.Cancel("Appeal reason cannot be empty.")
		return
	}

	// Route to appropriate handler based on appeal type
	appealType := session.AppealType.Get(s)
	switch appealType {
	case enum.AppealTypeRoblox:
		userIDStr := modalData.Text(constants.AppealUserInputCustomID)
		m.handleRobloxAppealSubmit(ctx, s, userIDStr, reason)
	case enum.AppealTypeDiscord:
		m.handleDiscordAppealSubmit(ctx, s, reason)
	default:
		ctx.Error("Invalid appeal type. Please try again.")
	}
}

// handleRobloxAppealSubmit processes a Roblox user ID appeal submission.
func (m *OverviewMenu) handleRobloxAppealSubmit(ctx *interaction.Context, s *session.Session, userIDStr, reason string) {
	// Parse profile URL if provided
	parsedURL, err := utils.ExtractUserIDFromURL(userIDStr)
	if err == nil {
		userIDStr = parsedURL
	}

	// Parse the user ID
	userID, err := strconv.ParseUint(userIDStr, 10, 64)
	if err != nil {
		ctx.Cancel("Invalid Roblox user ID format. Please enter a valid number.")
		return
	}

	// Check if the user ID already has a pending appeal
	exists, err := m.layout.db.Model().Appeal().HasPendingAppealByUserID(ctx.Context(), userID, enum.AppealTypeRoblox)
	if err != nil {
		m.layout.logger.Error("Failed to check pending appeals for user", zap.Error(err))
		ctx.Error("Failed to check pending appeals. Please try again.")
		return
	}
	if exists {
		ctx.Cancel("This user ID already has a pending appeal. Please wait for it to be reviewed.")
		return
	}

	// Check if the user is blacklisted
	blacklisted, err := m.layout.db.Model().Appeal().IsUserBlacklisted(ctx.Context(), userID, enum.AppealTypeRoblox)
	if err != nil {
		m.layout.logger.Error("Failed to check user blacklist status", zap.Error(err))
		ctx.Error("Failed to verify user status. Please try again.")
		return
	}
	if blacklisted {
		ctx.Cancel("This user has been blacklisted from submitting appeals.")
		return
	}

	// Check if the Discord user already has a pending appeal
	exists, err = m.layout.db.Model().Appeal().HasPendingAppealByRequester(
		ctx.Context(), uint64(ctx.Event().User().ID), enum.AppealTypeRoblox,
	)
	if err != nil {
		m.layout.logger.Error("Failed to check pending appeals", zap.Error(err))
		ctx.Error("Failed to check pending appeals. Please try again.")
		return
	}
	if exists {
		ctx.Cancel("You already have a pending appeal. Please wait for it to be reviewed.")
		return
	}

	// Check if the user ID has been previously rejected
	hasRejection, err := m.layout.db.Model().Appeal().HasPreviousRejection(ctx.Context(), userID, enum.AppealTypeRoblox)
	if err != nil {
		m.layout.logger.Error("Failed to check previous rejections", zap.Error(err))
		ctx.Error("Failed to check appeal history. Please try again.")
		return
	}
	if hasRejection {
		ctx.Cancel("This user ID has a rejected appeal recently. Please wait at least 3 days.")
		return
	}

	// Verify user exists in database
	user, err := m.layout.db.Service().User().GetUserByID(ctx.Context(), userIDStr, types.UserFieldAll)
	if err != nil {
		if errors.Is(err, types.ErrUserNotFound) {
			ctx.Cancel("Cannot submit appeal - user is not in our database.")
			return
		}
		m.layout.logger.Error("Failed to verify user status", zap.Error(err))
		ctx.Error("Failed to verify user status. Please try again.")
		return
	}

	// Only allow appeals for confirmed/flagged users
	if user.Status != enum.UserTypeConfirmed && user.Status != enum.UserTypeFlagged {
		ctx.Cancel("Cannot submit appeal - user must be confirmed or flagged.")
		return
	}

	// Show verification menu
	session.VerifyUserID.Set(s, userID)
	session.VerifyReason.Set(s, reason)
	ctx.Show(constants.AppealVerifyPageName, "")
}

// handleDiscordAppealSubmit processes a Discord user ID appeal submission.
func (m *OverviewMenu) handleDiscordAppealSubmit(ctx *interaction.Context, s *session.Session, reason string) {
	userID := uint64(ctx.Event().User().ID)

	// Check if user has any flags in the system
	totalGuilds, err := m.layout.db.Model().Sync().GetDiscordUserGuildCount(ctx.Context(), userID)
	if err != nil {
		m.layout.logger.Error("Failed to get Discord user guild count", zap.Error(err))
		ctx.Error("Failed to verify Discord user status. Please try again.")
		return
	}

	messageSummary, err := m.layout.db.Model().Message().GetUserInappropriateMessageSummary(ctx.Context(), userID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		m.layout.logger.Error("Failed to get message summary", zap.Error(err))
		ctx.Error("Failed to verify Discord user status. Please try again.")
		return
	}

	// Check if user is actually flagged
	if totalGuilds == 0 && (messageSummary == nil || messageSummary.MessageCount == 0) {
		ctx.Cancel("Your Discord account is not flagged in our system.")
		return
	}

	// Check if user is blacklisted
	blacklisted, err := m.layout.db.Model().Appeal().IsUserBlacklisted(ctx.Context(), userID, enum.AppealTypeDiscord)
	if err != nil {
		m.layout.logger.Error("Failed to check user blacklist status", zap.Error(err))
		ctx.Error("Failed to verify user status. Please try again.")
		return
	}
	if blacklisted {
		ctx.Cancel("Your account has been blacklisted from submitting appeals.")
		return
	}

	// Check for existing pending appeals
	exists, err := m.layout.db.Model().Appeal().HasPendingAppealByUserID(ctx.Context(), userID, enum.AppealTypeDiscord)
	if err != nil {
		m.layout.logger.Error("Failed to check pending appeals", zap.Error(err))
		ctx.Error("Failed to check pending appeals. Please try again.")
		return
	}
	if exists {
		ctx.Cancel("You already have a pending appeal. Please wait for it to be reviewed.")
		return
	}

	// Check for previous rejections
	hasRejection, err := m.layout.db.Model().Appeal().HasPreviousRejection(ctx.Context(), userID, enum.AppealTypeDiscord)
	if err != nil {
		m.layout.logger.Error("Failed to check previous rejections", zap.Error(err))
		ctx.Error("Failed to check appeal history. Please try again.")
		return
	}
	if hasRejection {
		ctx.Cancel("You have a rejected appeal recently. Please wait at least 3 days.")
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
	if err := m.layout.db.Model().Appeal().CreateAppeal(ctx.Context(), appeal, reason); err != nil {
		m.layout.logger.Error("Failed to create appeal", zap.Error(err))
		ctx.Error("Failed to submit appeal. Please try again.")
		return
	}

	session.AppealCursor.Delete(s)
	session.AppealPrevCursors.Delete(s)
	ctx.Show(constants.AppealOverviewPageName, "✅ Appeal submitted successfully!")

	// Log the appeal submission
	m.layout.db.Model().Activity().Log(ctx.Context(), &types.ActivityLog{
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
func (m *OverviewMenu) handleSearchAppealModalSubmit(ctx *interaction.Context, s *session.Session) {
	// Verify user is a reviewer
	if !s.BotSettings().IsReviewer(uint64(ctx.Event().User().ID)) {
		ctx.Cancel("Only reviewers can search appeals by ID.")
		return
	}

	// Get appeal ID input
	appealIDStr := ctx.Event().ModalData().Text(constants.AppealIDInputCustomID)
	appealID, err := strconv.ParseInt(appealIDStr, 10, 64)
	if err != nil {
		ctx.Cancel("Invalid appeal ID format. Please enter a valid number.")
		return
	}

	// Get the appeal
	appeal, err := m.layout.db.Model().Appeal().GetAppealByID(ctx.Context(), appealID)
	if err != nil {
		if errors.Is(err, types.ErrNoAppealsFound) {
			ctx.Cancel("Appeal not found.")
			return
		}
		m.layout.logger.Error("Failed to get appeal", zap.Error(err))
		ctx.Error("Failed to get appeal. Please try again.")
		return
	}

	// Show the selected appeal
	session.PaginationPage.Set(s, 0)
	session.AppealSelected.Set(s, appeal)
	ctx.Show(constants.AppealTicketPageName, "")
}

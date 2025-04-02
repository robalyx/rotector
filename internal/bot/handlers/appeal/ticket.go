package appeal

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/disgoorg/disgo/discord"
	apiTypes "github.com/jaxron/roapi.go/pkg/api/types"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/interaction"
	"github.com/robalyx/rotector/internal/bot/core/session"
	view "github.com/robalyx/rotector/internal/bot/views/appeal"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/database/types/enum"
	"go.uber.org/zap"
)

// TicketMenu handles the display and interaction logic for individual appeal tickets.
type TicketMenu struct {
	layout *Layout
	page   *interaction.Page
}

// NewTicketMenu creates a new ticket menu.
func NewTicketMenu(layout *Layout) *TicketMenu {
	m := &TicketMenu{layout: layout}
	m.page = &interaction.Page{
		Name: constants.AppealTicketPageName,
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return view.NewTicketBuilder(s).Build()
		},
		CleanupHandlerFunc: m.Cleanup,
		ShowHandlerFunc:    m.Show,
		ButtonHandlerFunc:  m.handleButton,
		SelectHandlerFunc:  m.handleSelectMenu,
		ModalHandlerFunc:   m.handleModal,
	}
	return m
}

// Show prepares and displays the appeal ticket interface.
func (m *TicketMenu) Show(ctx *interaction.Context, s *session.Session) {
	// Use fresh appeal data from database
	appeal, err := m.useFreshAppeal(ctx.Context(), s)
	if err != nil {
		ctx.Error("Failed to use fresh appeal. Please try again.")
		return
	}

	// If appeal is pending, check if user's status has changed
	if appeal.Status == enum.AppealStatusPending {
		shouldContinue := m.handlePendingAppeal(ctx, s, appeal)
		if !shouldContinue {
			return
		}
	}

	// Get messages for the appeal
	messages, err := m.layout.db.Model().Appeal().GetAppealMessages(ctx.Context(), appeal.ID)
	if err != nil {
		m.layout.logger.Error("Failed to get appeal messages", zap.Error(err))
		ctx.Error("Failed to load appeal messages. Please try again.")
		return
	}

	// Get rejected appeals count for this user
	rejectedCount, err := m.layout.db.Model().Appeal().GetRejectedAppealsCount(ctx.Context(), appeal.UserID)
	if err != nil {
		m.layout.logger.Error("Failed to get rejected appeals count", zap.Error(err))
	}
	session.AppealRejectedCount.Set(s, rejectedCount)

	// Calculate total pages
	totalPages := max((len(messages)-1)/constants.AppealMessagesPerPage, 0)

	// Store data in session
	session.AppealMessages.Set(s, messages)
	session.PaginationTotalPages.Set(s, totalPages)
}

// handlePendingAppeal checks the status of a pending appeal and handles any necessary auto-actions.
// Returns false if the appeal was auto-handled and the caller should stop processing.
func (m *TicketMenu) handlePendingAppeal(ctx *interaction.Context, s *session.Session, appeal *types.FullAppeal) bool {
	switch appeal.Type {
	case enum.AppealTypeRoblox:
		// Get current Roblox user status
		user, err := m.layout.db.Service().User().GetUserByID(
			ctx.Context(), strconv.FormatUint(appeal.UserID, 10), types.UserFieldAll,
		)
		if err != nil {
			if errors.Is(err, types.ErrUserNotFound) {
				// User no longer exists, auto-reject the appeal
				if err := m.layout.db.Model().Appeal().RejectAppeal(
					ctx.Context(), appeal.ID, appeal.Timestamp, "Roblox user no longer exists in database.",
				); err != nil {
					m.layout.logger.Error("Failed to auto-reject appeal", zap.Error(err))
				}

				ResetAppealData(s)
				ctx.Reload("Appeal automatically closed: Roblox user no longer exists in database.")
				return false
			}

			m.layout.logger.Error("Failed to verify Roblox user status", zap.Error(err))
			ctx.Error("Failed to verify user status. Please try again.")
			return false
		}

		if user.Status != enum.UserTypeConfirmed && user.Status != enum.UserTypeFlagged {
			// User is no longer flagged or confirmed, auto-reject the appeal
			reason := fmt.Sprintf("Roblox user status changed to %s", user.Status)
			if err := m.layout.db.Model().Appeal().RejectAppeal(
				ctx.Context(), appeal.ID, appeal.Timestamp, reason,
			); err != nil {
				m.layout.logger.Error("Failed to auto-reject appeal", zap.Error(err))
			}

			ResetAppealData(s)
			ctx.Reload("Appeal automatically closed: Roblox user status changed to " + user.Status.String())
			return false
		}

	case enum.AppealTypeDiscord:
		// Check if Discord user still has flags
		totalGuilds, err := m.layout.db.Model().Sync().GetDiscordUserGuildCount(ctx.Context(), appeal.UserID)
		if err != nil {
			m.layout.logger.Error("Failed to get Discord user guild count", zap.Error(err))
			ctx.Error("Failed to verify Discord user status. Please try again.")
			return false
		}

		messageSummary, err := m.layout.db.Model().Message().GetUserInappropriateMessageSummary(
			ctx.Context(), appeal.UserID,
		)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			m.layout.logger.Error("Failed to get message summary", zap.Error(err))
			ctx.Error("Failed to verify Discord user status. Please try again.")
			return false
		}

		// Check if user is still flagged
		if totalGuilds == 0 && (messageSummary == nil || messageSummary.MessageCount == 0) {
			// User is no longer flagged, auto-accept the appeal
			reason := "Discord user is no longer flagged in the system"
			if err := m.layout.db.Model().Appeal().AcceptAppeal(
				ctx.Context(), appeal.ID, appeal.Timestamp, reason,
			); err != nil {
				m.layout.logger.Error("Failed to auto-accept appeal", zap.Error(err))
			}

			ResetAppealData(s)
			ctx.Reload("Appeal automatically accepted: Discord user is no longer flagged.")
			return false
		}
	}

	return true
}

// Cleanup handles the cleanup of the appeal ticket interface.
func (m *TicketMenu) Cleanup(s *session.Session) {
	session.AppealMessages.Delete(s)
	session.PaginationTotalPages.Delete(s)
	session.PaginationPage.Delete(s)
}

// handleButton processes button interactions.
func (m *TicketMenu) handleButton(ctx *interaction.Context, s *session.Session, customID string) {
	action := session.ViewerAction(customID)
	switch action {
	case session.ViewerFirstPage, session.ViewerPrevPage, session.ViewerNextPage, session.ViewerLastPage:
		totalPages := session.PaginationTotalPages.Get(s)
		page := action.ParsePageAction(s, totalPages)

		session.PaginationPage.Set(s, page)
		ctx.Cancel("")
		return
	}

	switch customID {
	case constants.BackButtonCustomID:
		ctx.NavigateBack("")
	}
}

// handleSelectMenu processes select menu interactions.
func (m *TicketMenu) handleSelectMenu(ctx *interaction.Context, s *session.Session, customID, option string) {
	// Use fresh appeal data from database
	appeal, err := m.useFreshAppeal(ctx.Context(), s)
	if err != nil {
		ctx.Error("Failed to use fresh appeal. Please try again.")
		return
	}

	switch customID {
	case constants.AppealActionSelectID:
		switch option {
		case constants.AppealRespondButtonCustomID:
			m.handleRespond(ctx, s)
		case constants.AppealLookupUserButtonCustomID:
			m.handleLookupUser(ctx, s, appeal)
		case constants.AppealClaimButtonCustomID:
			m.handleClaimAppeal(ctx, s, appeal)
		case constants.AcceptAppealButtonCustomID:
			m.handleAcceptAppeal(ctx, s)
		case constants.RejectAppealButtonCustomID:
			m.handleRejectAppeal(ctx, s)
		case constants.AppealCloseButtonCustomID:
			m.handleCloseAppeal(ctx, s, appeal)
		case constants.ReopenAppealButtonCustomID:
			m.handleReopenAppeal(ctx, s, appeal)
		case constants.DeleteUserDataButtonCustomID:
			m.handleDeleteUserData(ctx, s)
		case constants.BlacklistUserButtonCustomID:
			m.handleBlacklistUser(ctx, s)
		}
	}
}

// handleRespond opens a modal for responding to the appeal.
func (m *TicketMenu) handleRespond(ctx *interaction.Context, s *session.Session) {
	appeal := session.AppealSelected.Get(s)
	userID := uint64(ctx.Event().User().ID)
	isReviewer := s.BotSettings().IsReviewer(userID)

	if userID != appeal.RequesterID {
		// Non-reviewer is not the ticket owner
		if !isReviewer {
			ctx.Error("You don't have permission to respond to this appeal")
			return
		}
		// Reviewer must have claimed the appeal
		if appeal.ClaimedBy != userID {
			ctx.Error("You must claim this appeal before responding")
			return
		}
	}

	modal := discord.NewModalCreateBuilder().
		SetCustomID(constants.AppealRespondModalCustomID).
		SetTitle("Respond to Appeal").
		AddActionRow(
			discord.NewTextInput(constants.AppealReasonInputCustomID, discord.TextInputStyleParagraph, "Message").
				WithRequired(true).
				WithMaxLength(512).
				WithPlaceholder("Type your response..."),
		)

	ctx.Modal(modal)
}

// handleLookupUser opens the review menu for the appealed user.
func (m *TicketMenu) handleLookupUser(ctx *interaction.Context, s *session.Session, appeal *types.FullAppeal) {
	// Verify user is a reviewer
	userID := uint64(ctx.Event().User().ID)
	if !s.BotSettings().IsReviewer(userID) {
		ctx.Error("Only reviewers can lookup users")
		return
	}

	switch appeal.Type {
	case enum.AppealTypeRoblox:
		// Get Roblox user from database
		user, err := m.layout.db.Service().User().GetUserByID(
			ctx.Context(), strconv.FormatUint(appeal.UserID, 10), types.UserFieldAll,
		)
		if err != nil {
			if errors.Is(err, types.ErrUserNotFound) {
				ctx.Cancel("Failed to find Roblox user. They may not be in our database.")
				return
			}
			m.layout.logger.Error("Failed to fetch Roblox user for review", zap.Error(err))
			ctx.Error("Failed to fetch user for review. Please try again.")
			return
		}

		// Store user in session and show review menu
		session.UserTarget.Set(s, user)
		ctx.Show(constants.UserReviewPageName, "")

		// Log the lookup action
		m.layout.db.Model().Activity().Log(ctx.Context(), &types.ActivityLog{
			ActivityTarget: types.ActivityTarget{
				UserID: user.ID,
			},
			ReviewerID:        userID,
			ActivityType:      enum.ActivityTypeUserLookup,
			ActivityTimestamp: time.Now(),
			Details:           map[string]any{},
		})

	case enum.AppealTypeDiscord:
		// Lookup the Discord user
		session.DiscordUserLookupID.Set(s, appeal.UserID)
		ctx.Show(constants.GuildLookupPageName, "")

		// Log the lookup action
		m.layout.db.Model().Activity().Log(ctx.Context(), &types.ActivityLog{
			ActivityTarget: types.ActivityTarget{
				UserID: appeal.UserID,
			},
			ReviewerID:        userID,
			ActivityType:      enum.ActivityTypeUserLookupDiscord,
			ActivityTimestamp: time.Now(),
			Details:           map[string]any{},
		})
	}
}

// handleClaimAppeal handles claiming an appeal by a reviewer.
func (m *TicketMenu) handleClaimAppeal(ctx *interaction.Context, s *session.Session, appeal *types.FullAppeal) {
	// Verify user is a reviewer
	userID := uint64(ctx.Event().User().ID)
	if !s.BotSettings().IsReviewer(userID) {
		ctx.Error("Only reviewers can claim appeals")
		return
	}

	// Update the appeal in the database
	appeal.ClaimedBy = userID
	appeal.ClaimedAt = time.Now()

	if err := m.layout.db.Model().Appeal().ClaimAppeal(ctx.Context(), appeal.ID, appeal.Timestamp, userID); err != nil {
		m.layout.logger.Error("Failed to claim appeal", zap.Error(err))
		ctx.Error("Failed to claim appeal. Please try again.")
		return
	}

	// Reload the appeal
	session.AppealSelected.Set(s, appeal)
	ctx.Reload("Appeal claimed successfully.")

	// Log the claim action
	m.layout.db.Model().Activity().Log(ctx.Context(), &types.ActivityLog{
		ActivityTarget: types.ActivityTarget{
			UserID: appeal.UserID,
		},
		ReviewerID:        userID,
		ActivityType:      enum.ActivityTypeAppealClaimed,
		ActivityTimestamp: time.Now(),
		Details: map[string]any{
			"appeal_id": appeal.ID,
		},
	})
}

// handleAcceptAppeal opens a modal for accepting the appeal with a reason.
func (m *TicketMenu) handleAcceptAppeal(ctx *interaction.Context, s *session.Session) {
	appeal := session.AppealSelected.Get(s)
	userID := uint64(ctx.Event().User().ID)

	// Verify user is a reviewer
	if !s.BotSettings().IsReviewer(userID) {
		ctx.Error("Only reviewers can accept appeals")
		return
	}

	// Verify user has claimed the appeal
	if appeal.ClaimedBy != userID {
		ctx.Error("You must claim this appeal before accepting it")
		return
	}

	modal := discord.NewModalCreateBuilder().
		SetCustomID(constants.AcceptAppealModalCustomID).
		SetTitle("Accept Appeal & Delete Data").
		AddActionRow(
			discord.NewTextInput(constants.AppealReasonInputCustomID, discord.TextInputStyleParagraph, "Accept Reason").
				WithRequired(true).
				WithPlaceholder("Enter the reason for accepting this appeal and deleting user data..."),
		)

	ctx.Modal(modal)
}

// handleRejectAppeal opens a modal for rejecting the appeal with a reason.
func (m *TicketMenu) handleRejectAppeal(ctx *interaction.Context, s *session.Session) {
	appeal := session.AppealSelected.Get(s)
	userID := uint64(ctx.Event().User().ID)

	// Verify user is a reviewer
	if !s.BotSettings().IsReviewer(userID) {
		ctx.Error("Only reviewers can reject appeals")
		return
	}

	// Verify user has claimed the appeal
	if appeal.ClaimedBy != userID {
		ctx.Error("You must claim this appeal before rejecting it")
		return
	}

	modal := discord.NewModalCreateBuilder().
		SetCustomID(constants.RejectAppealModalCustomID).
		SetTitle("Reject Appeal").
		AddActionRow(
			discord.NewTextInput(constants.AppealReasonInputCustomID, discord.TextInputStyleParagraph, "Reject Reason").
				WithRequired(true).
				WithPlaceholder("Enter the reason for rejecting this appeal..."),
		)

	ctx.Modal(modal)
}

// handleCloseAppeal handles the user closing their own appeal ticket.
func (m *TicketMenu) handleCloseAppeal(ctx *interaction.Context, s *session.Session, appeal *types.FullAppeal) {
	// Verify the user is the appeal creator
	userID := uint64(ctx.Event().User().ID)
	if userID != appeal.RequesterID {
		ctx.Cancel("Only the appeal creator can close this ticket.")
		return
	}

	// Close the appeal by rejecting it
	err := m.layout.db.Model().Appeal().RejectAppeal(
		ctx.Context(), appeal.ID, appeal.Timestamp, "Closed by appeal creator",
	)
	if err != nil {
		m.layout.logger.Error("Failed to close appeal",
			zap.Error(err),
			zap.Int64("appealID", appeal.ID))
		ctx.Error("Failed to close appeal. Please try again.")
		return
	}

	// Return to overview
	ResetAppealData(s)
	ctx.NavigateBack("Appeal closed successfully.")

	// Log the appeal closing
	m.layout.db.Model().Activity().Log(ctx.Context(), &types.ActivityLog{
		ActivityTarget: types.ActivityTarget{
			UserID: appeal.UserID,
		},
		ReviewerID:        userID,
		ActivityType:      enum.ActivityTypeAppealClosed,
		ActivityTimestamp: time.Now(),
		Details: map[string]any{
			"appeal_id": appeal.ID,
		},
	})
}

// handleReopenAppeal handles reopening a closed appeal.
func (m *TicketMenu) handleReopenAppeal(ctx *interaction.Context, s *session.Session, appeal *types.FullAppeal) {
	// Verify user is a reviewer
	reviewerID := uint64(ctx.Event().User().ID)
	if !s.BotSettings().IsReviewer(reviewerID) {
		ctx.Cancel("Only reviewers can reopen appeals.")
		return
	}

	// Verify appeal is rejected or accepted
	if appeal.Status != enum.AppealStatusRejected && appeal.Status != enum.AppealStatusAccepted {
		ctx.Cancel("Only rejected or accepted appeals can be reopened.")
		return
	}

	// Reopen and claim the appeal
	if err := m.layout.db.Model().Appeal().ReopenAppeal(ctx.Context(), appeal.ID, appeal.Timestamp, reviewerID); err != nil {
		m.layout.logger.Error("Failed to reopen appeal",
			zap.Error(err),
			zap.Int64("appealID", appeal.ID))
		ctx.Error("Failed to reopen appeal. Please try again.")
		return
	}

	// Return to overview
	ResetAppealData(s)
	ctx.NavigateBack("Appeal reopened and claimed successfully.")

	// Log the appeal reopening
	m.layout.db.Model().Activity().Log(ctx.Context(), &types.ActivityLog{
		ActivityTarget: types.ActivityTarget{
			UserID: appeal.UserID,
		},
		ReviewerID:        reviewerID,
		ActivityType:      enum.ActivityTypeAppealReopened,
		ActivityTimestamp: time.Now(),
		Details: map[string]any{
			"appeal_id": appeal.ID,
		},
	})
}

// handleDeleteUserData opens a modal for confirming user data deletion.
func (m *TicketMenu) handleDeleteUserData(ctx *interaction.Context, s *session.Session) {
	appeal := session.AppealSelected.Get(s)
	userID := uint64(ctx.Event().User().ID)

	// Verify user is a reviewer
	if !s.BotSettings().IsReviewer(userID) {
		ctx.Error("Only reviewers can delete user data")
		return
	}

	// Verify user has claimed the appeal
	if appeal.ClaimedBy != userID {
		ctx.Error("You must claim this appeal before deleting user data")
		return
	}

	modal := discord.NewModalCreateBuilder().
		SetCustomID(constants.DeleteUserDataModalCustomID).
		SetTitle("Delete User Data").
		AddActionRow(
			discord.NewTextInput(constants.DeleteUserDataReasonInputCustomID, discord.TextInputStyleParagraph, "Deletion Reason").
				WithRequired(true).
				WithPlaceholder("Enter the reason for deleting this user's data...").
				WithMinLength(10).
				WithMaxLength(512),
		)

	ctx.Modal(modal)
}

// handleBlacklistUser opens a modal for confirming user blacklist.
func (m *TicketMenu) handleBlacklistUser(ctx *interaction.Context, s *session.Session) {
	appeal := session.AppealSelected.Get(s)
	userID := uint64(ctx.Event().User().ID)

	// Verify user is a reviewer
	if !s.BotSettings().IsReviewer(userID) {
		ctx.Error("Only reviewers can blacklist users")
		return
	}

	// Verify user has claimed the appeal
	if appeal.ClaimedBy != userID {
		ctx.Error("You must claim this appeal before blacklisting the user")
		return
	}

	modal := discord.NewModalCreateBuilder().
		SetCustomID(constants.BlacklistUserModalCustomID).
		SetTitle("Blacklist User").
		AddActionRow(
			discord.NewTextInput(constants.BlacklistUserReasonInputCustomID, discord.TextInputStyleParagraph, "Blacklist Reason").
				WithRequired(true).
				WithPlaceholder("Enter the reason for blacklisting this user from appeals...").
				WithMinLength(10).
				WithMaxLength(512),
		)

	ctx.Modal(modal)
}

// handleModal processes modal submissions.
func (m *TicketMenu) handleModal(ctx *interaction.Context, s *session.Session) {
	// Use fresh appeal data from database
	appeal, err := m.useFreshAppeal(ctx.Context(), s)
	if err != nil {
		ctx.Error("Failed to use fresh appeal. Please try again.")
		return
	}

	switch ctx.Event().CustomID() {
	case constants.AppealRespondModalCustomID:
		m.handleRespondModalSubmit(ctx, s, appeal)
	case constants.AcceptAppealModalCustomID:
		m.handleAcceptModalSubmit(ctx, s, appeal)
	case constants.RejectAppealModalCustomID:
		m.handleRejectModalSubmit(ctx, s, appeal)
	case constants.DeleteUserDataModalCustomID:
		m.handleDeleteUserDataModalSubmit(ctx, s, appeal)
	case constants.BlacklistUserModalCustomID:
		m.handleBlacklistUserModalSubmit(ctx, s, appeal)
	}
}

// handleRespondModalSubmit processes the response modal submission.
func (m *TicketMenu) handleRespondModalSubmit(ctx *interaction.Context, s *session.Session, appeal *types.FullAppeal) {
	// Only allow responses for pending appeals
	if appeal.Status != enum.AppealStatusPending {
		ctx.Cancel("Cannot respond to a closed appeal.")
		return
	}

	// Check if response is empty
	content := ctx.Event().ModalData().Text(constants.AppealReasonInputCustomID)
	if content == "" {
		ctx.Cancel("Response cannot be empty.")
		return
	}

	// Get user role and check rate limit
	userID := uint64(ctx.Event().User().ID)
	role := enum.MessageRoleUser

	if s.BotSettings().IsReviewer(userID) {
		role = enum.MessageRoleModerator
	} else {
		// Check if user is allowed to send a message
		messages := session.AppealMessages.Get(s)
		if allowed, errorMsg := m.isMessageAllowed(messages, userID); !allowed {
			ctx.Error(errorMsg)
			return
		}
	}

	// Create new message
	message := &types.AppealMessage{
		AppealID:  appeal.ID,
		UserID:    userID,
		Role:      role,
		Content:   content,
		CreatedAt: time.Now(),
	}

	// Save message and update appeal
	err := m.layout.db.Model().Appeal().AddAppealMessage(ctx.Context(), message, appeal.ID, appeal.Timestamp)
	if err != nil {
		m.layout.logger.Error("Failed to add appeal message", zap.Error(err))
		ctx.Error("Failed to save response. Please try again.")
		return
	}

	// Refresh the ticket view
	ctx.Reload("Response added successfully.")
}

// handleAcceptModalSubmit processes the accept appeal submission.
func (m *TicketMenu) handleAcceptModalSubmit(ctx *interaction.Context, s *session.Session, appeal *types.FullAppeal) {
	// Verify user is a reviewer
	reviewerID := uint64(ctx.Event().User().ID)
	if !s.BotSettings().IsReviewer(reviewerID) {
		ctx.Cancel("Only reviewers can accept appeals.")
		return
	}

	// Prevent accepting already processed appeals
	if appeal.Status != enum.AppealStatusPending {
		ctx.Error("This appeal has already been processed.")
		return
	}

	reason := ctx.Event().ModalData().Text(constants.AppealReasonInputCustomID)
	if reason == "" {
		ctx.Cancel("Accept reason cannot be empty.")
		return
	}

	// Handle appeal based on type
	var err error
	switch appeal.Type {
	case enum.AppealTypeRoblox:
		err = m.handleAcceptRobloxAppeal(ctx.Context(), appeal, reviewerID, reason)
	case enum.AppealTypeDiscord:
		err = m.handleAcceptDiscordAppeal(ctx.Context(), appeal, reviewerID, reason)
	}

	if err != nil {
		m.layout.logger.Error("Failed to process appeal acceptance", zap.Error(err))
		ctx.Error("Failed to process appeal. Please try again.")
		return
	}

	// Accept the appeal
	err = m.layout.db.Model().Appeal().AcceptAppeal(ctx.Context(), appeal.ID, appeal.Timestamp, reason)
	if err != nil {
		m.layout.logger.Error("Failed to accept appeal", zap.Error(err))
		ctx.Error("Failed to accept appeal. Please try again.")
		return
	}

	// Refresh the ticket view
	ResetAppealData(s)
	ctx.NavigateBack("Appeal accepted and user data deleted.")

	// Log the appeal acceptance
	m.layout.db.Model().Activity().Log(ctx.Context(), &types.ActivityLog{
		ActivityTarget: types.ActivityTarget{
			UserID: appeal.UserID,
		},
		ReviewerID:        reviewerID,
		ActivityType:      enum.ActivityTypeAppealAccepted,
		ActivityTimestamp: time.Now(),
		Details: map[string]any{
			"appeal_id":    appeal.ID,
			"appeal_type":  appeal.Type.String(),
			"reason":       reason,
			"data_deleted": true,
		},
	})
}

// handleAcceptRobloxAppeal handles the acceptance of a Roblox user appeal.
func (m *TicketMenu) handleAcceptRobloxAppeal(
	ctx context.Context, appeal *types.FullAppeal, reviewerID uint64, reason string,
) error {
	// Get Roblox user to clear
	user, err := m.layout.db.Service().User().GetUserByID(
		ctx, strconv.FormatUint(appeal.UserID, 10), types.UserFieldAll,
	)
	if err != nil {
		return fmt.Errorf("failed to get Roblox user: %w", err)
	}

	// Clear the user if not already cleared
	if user.Status != enum.UserTypeCleared {
		if err := m.layout.db.Service().User().ClearUser(ctx, user); err != nil {
			return fmt.Errorf("failed to clear user: %w", err)
		}
	}

	// Redact Roblox user data and log the action
	if err := m.redactRobloxUserData(ctx, user, reviewerID, reason, appeal.ID); err != nil {
		return fmt.Errorf("failed to redact Roblox user data: %w", err)
	}

	return nil
}

// handleAcceptDiscordAppeal handles the acceptance of a Discord user appeal.
func (m *TicketMenu) handleAcceptDiscordAppeal(
	ctx context.Context, appeal *types.FullAppeal, reviewerID uint64, reason string,
) error {
	// Delete all inappropriate messages and guild memberships
	if err := m.layout.db.Model().Message().DeleteUserMessages(ctx, appeal.UserID); err != nil {
		return fmt.Errorf("failed to delete Discord user messages: %w", err)
	}

	if err := m.layout.db.Model().Sync().DeleteUserGuildMemberships(ctx, appeal.UserID); err != nil {
		return fmt.Errorf("failed to delete Discord user guild memberships: %w", err)
	}

	// Add user to whitelist
	if err := m.layout.db.Model().Sync().WhitelistDiscordUser(ctx, &types.DiscordUserWhitelist{
		UserID:        appeal.UserID,
		WhitelistedAt: time.Now(),
		Reason:        reason,
		ReviewerID:    reviewerID,
		AppealID:      appeal.ID,
	}); err != nil {
		return fmt.Errorf("failed to whitelist Discord user: %w", err)
	}

	// Mark user data as redacted
	if err := m.layout.db.Model().Sync().MarkUserDataRedacted(ctx, appeal.UserID); err != nil {
		return fmt.Errorf("failed to mark Discord user data as redacted: %w", err)
	}

	// Log the data deletion
	m.layout.db.Model().Activity().Log(ctx, &types.ActivityLog{
		ActivityTarget: types.ActivityTarget{
			UserID: appeal.UserID,
		},
		ReviewerID:        reviewerID,
		ActivityType:      enum.ActivityTypeUserDataDeleted,
		ActivityTimestamp: time.Now(),
		Details: map[string]any{
			"reason":      reason,
			"appeal_id":   appeal.ID,
			"appeal_type": enum.AppealTypeDiscord.String(),
			"whitelisted": true,
		},
	})

	return nil
}

// handleRejectModalSubmit processes the reject appeal submission.
func (m *TicketMenu) handleRejectModalSubmit(ctx *interaction.Context, s *session.Session, appeal *types.FullAppeal) {
	// Verify user is a reviewer
	reviewerID := uint64(ctx.Event().User().ID)
	if !s.BotSettings().IsReviewer(reviewerID) {
		ctx.Error("Only reviewers can reject appeals.")
		return
	}

	// Prevent rejecting already processed appeals
	if appeal.Status != enum.AppealStatusPending {
		ctx.Cancel("This appeal has already been processed.")
		return
	}

	reason := ctx.Event().ModalData().Text(constants.AppealReasonInputCustomID)
	if reason == "" {
		ctx.Cancel("Reject reason cannot be empty.")
		return
	}

	// Reject the appeal
	err := m.layout.db.Model().Appeal().RejectAppeal(ctx.Context(), appeal.ID, appeal.Timestamp, reason)
	if err != nil {
		m.layout.logger.Error("Failed to reject appeal", zap.Error(err))
		ctx.Error("Failed to reject appeal. Please try again.")
		return
	}

	// Refresh the ticket view
	ResetAppealData(s)
	ctx.NavigateBack("Appeal rejected.")

	// Log the appeal rejection
	m.layout.db.Model().Activity().Log(ctx.Context(), &types.ActivityLog{
		ActivityTarget: types.ActivityTarget{
			UserID: appeal.UserID,
		},
		ReviewerID:        reviewerID,
		ActivityType:      enum.ActivityTypeAppealRejected,
		ActivityTimestamp: time.Now(),
		Details: map[string]any{
			"reason":    reason,
			"appeal_id": appeal.ID,
		},
	})
}

// handleDeleteUserDataModalSubmit processes the data deletion confirmation.
func (m *TicketMenu) handleDeleteUserDataModalSubmit(ctx *interaction.Context, s *session.Session, appeal *types.FullAppeal) {
	// Verify user is a reviewer
	reviewerID := uint64(ctx.Event().User().ID)
	if !s.BotSettings().IsReviewer(reviewerID) {
		ctx.Error("Only reviewers can process data deletion requests.")
		return
	}

	// Prevent processing already processed appeals
	if appeal.Status != enum.AppealStatusPending {
		ctx.Cancel("This appeal has already been processed.")
		return
	}

	// Get deletion reason
	reason := ctx.Event().ModalData().Text(constants.DeleteUserDataReasonInputCustomID)
	if reason == "" {
		ctx.Cancel("Deletion reason cannot be empty.")
		return
	}

	var redactErr error
	switch appeal.Type {
	case enum.AppealTypeRoblox:
		// Get Roblox user from database
		user, err := m.layout.db.Service().User().GetUserByID(
			ctx.Context(), strconv.FormatUint(appeal.UserID, 10), types.UserFieldAll,
		)
		if err != nil {
			if errors.Is(err, types.ErrUserNotFound) {
				ctx.Error("Failed to find Roblox user. They may no longer exist in our database.")
				return
			}
			m.layout.logger.Error("Failed to get Roblox user for deletion", zap.Error(err))
			ctx.Error("Failed to get user information. Please try again.")
			return
		}

		// Redact Roblox user data and log the action
		redactErr = m.redactRobloxUserData(ctx.Context(), user, reviewerID, reason, appeal.ID)

	case enum.AppealTypeDiscord:
		// Redact Discord user data and log the action
		redactErr = m.redactDiscordUserData(ctx.Context(), appeal.UserID, reviewerID, reason, appeal.ID)
	}

	if redactErr != nil {
		m.layout.logger.Error("Failed to redact user data", zap.Error(redactErr))
		ctx.Error("Failed to process user data. Please try again.")
		return
	}

	// Accept the appeal
	if err := m.layout.db.Model().Appeal().AcceptAppeal(
		ctx.Context(), appeal.ID, appeal.Timestamp,
		"Data deletion request processed: "+reason,
	); err != nil {
		m.layout.logger.Error("Failed to accept appeal after data deletion", zap.Error(err))
		ctx.Error("Failed to update appeal status. Please try again.")
		return
	}

	// Refresh the ticket view
	ResetAppealData(s)
	ctx.NavigateBack("User data has been deleted and appeal accepted.")
}

// handleBlacklistUserModalSubmit processes the blacklist confirmation modal submission.
func (m *TicketMenu) handleBlacklistUserModalSubmit(ctx *interaction.Context, s *session.Session, appeal *types.FullAppeal) {
	// Verify user is a reviewer
	reviewerID := uint64(ctx.Event().User().ID)
	if !s.BotSettings().IsReviewer(reviewerID) {
		ctx.Error("Only reviewers can blacklist users.")
		return
	}

	// Get blacklist reason
	reason := ctx.Event().ModalData().Text(constants.BlacklistUserReasonInputCustomID)
	if reason == "" {
		ctx.Cancel("Blacklist reason cannot be empty.")
		return
	}

	now := time.Now()

	// Create blacklist entry
	blacklist := &types.AppealBlacklist{
		UserID:     appeal.UserID,
		Type:       appeal.Type,
		ReviewerID: reviewerID,
		Reason:     reason,
		CreatedAt:  now,
		AppealID:   appeal.ID,
	}

	if err := m.layout.db.Model().Appeal().BlacklistUser(ctx.Context(), blacklist); err != nil {
		m.layout.logger.Error("Failed to blacklist user", zap.Error(err))
		ctx.Error("Failed to blacklist user. Please try again.")
		return
	}

	// Reject the appeal with the blacklist reason
	if err := m.layout.db.Model().Appeal().RejectAppeal(
		ctx.Context(), appeal.ID, appeal.Timestamp,
		"User blacklisted from appeals: "+reason,
	); err != nil {
		m.layout.logger.Error("Failed to reject appeal after blacklisting", zap.Error(err))
		ctx.Error("Failed to update appeal status. Please try again.")
		return
	}

	// Return to overview
	ResetAppealData(s)
	ctx.NavigateBack("User has been blacklisted from submitting appeals.")

	// Log the blacklist action
	m.layout.db.Model().Activity().Log(ctx.Context(), &types.ActivityLog{
		ActivityTarget: types.ActivityTarget{
			UserID: appeal.UserID,
		},
		ReviewerID:        reviewerID,
		ActivityType:      enum.ActivityTypeUserBlacklisted,
		ActivityTimestamp: now,
		Details: map[string]any{
			"reason":      reason,
			"appeal_id":   appeal.ID,
			"appeal_type": appeal.Type.String(),
		},
	})
}

// redactRobloxUserData handles redacting a Roblox user's data and logs the action.
func (m *TicketMenu) redactRobloxUserData(
	ctx context.Context, user *types.ReviewUser, reviewerID uint64, reason string, appealID int64,
) error {
	// Redact user data
	user.Name = "-----"
	user.DisplayName = "-----"
	user.Description = "[data deleted per user request]"
	user.Groups = []*apiTypes.UserGroupRoles{}
	user.Outfits = []*apiTypes.Outfit{}
	user.Friends = []*apiTypes.ExtendedFriend{}
	user.Games = []*apiTypes.Game{}
	user.IsDeleted = true
	user.ThumbnailURL = ""
	user.LastThumbnailUpdate = time.Now()

	// Update the user with redacted data
	if err := m.layout.db.Service().User().SaveUsers(
		ctx, map[uint64]*types.User{user.ID: &user.User},
	); err != nil {
		return fmt.Errorf("failed to save redacted user data: %w", err)
	}

	// Log the data deletion
	m.layout.db.Model().Activity().Log(ctx, &types.ActivityLog{
		ActivityTarget: types.ActivityTarget{
			UserID: user.ID,
		},
		ReviewerID:        reviewerID,
		ActivityType:      enum.ActivityTypeUserDataDeleted,
		ActivityTimestamp: time.Now(),
		Details: map[string]any{
			"reason":      reason,
			"appeal_id":   appealID,
			"appeal_type": enum.AppealTypeRoblox.String(),
		},
	})

	return nil
}

// redactDiscordUserData handles redacting a Discord user's data and logs the action.
func (m *TicketMenu) redactDiscordUserData(
	ctx context.Context, userID uint64, reviewerID uint64, reason string, appealID int64,
) error {
	// Redact message content
	if err := m.layout.db.Model().Message().RedactUserMessages(ctx, userID); err != nil {
		return fmt.Errorf("failed to redact Discord user messages: %w", err)
	}

	// Mark user data as redacted
	if err := m.layout.db.Model().Sync().MarkUserDataRedacted(ctx, userID); err != nil {
		return fmt.Errorf("failed to mark Discord user data as redacted: %w", err)
	}

	// Log the data deletion
	m.layout.db.Model().Activity().Log(ctx, &types.ActivityLog{
		ActivityTarget: types.ActivityTarget{
			UserID: userID,
		},
		ReviewerID:        reviewerID,
		ActivityType:      enum.ActivityTypeUserDataDeleted,
		ActivityTimestamp: time.Now(),
		Details: map[string]any{
			"reason":      reason,
			"appeal_id":   appealID,
			"appeal_type": enum.AppealTypeDiscord.String(),
		},
	})

	return nil
}

// isMessageAllowed checks if a user is allowed to send a message based on spam prevention rules.
func (m *TicketMenu) isMessageAllowed(messages []*types.AppealMessage, userID uint64) (bool, string) {
	// Check if the last 3 messages were from this user
	consecutiveUserMessages := 0
	for i := len(messages) - 1; i >= 0 && i > len(messages)-4; i-- {
		if messages[i].UserID == userID && messages[i].Role == enum.MessageRoleUser {
			consecutiveUserMessages++
		} else {
			break
		}
	}

	if consecutiveUserMessages >= 3 {
		return false, "Please wait for a moderator to respond before sending more messages."
	}

	// Check rate limit
	if len(messages) > 0 {
		lastMsg := messages[len(messages)-1]
		if lastMsg.UserID == userID &&
			lastMsg.Role == enum.MessageRoleUser &&
			time.Since(lastMsg.CreatedAt) < time.Minute {
			return false, "Please wait at least 1 minute between messages."
		}
	}

	return true, ""
}

// useFreshAppeal gets a fresh appeal from the database instead of using the cached version.
func (m *TicketMenu) useFreshAppeal(ctx context.Context, s *session.Session) (*types.FullAppeal, error) {
	appeal := session.AppealSelected.Get(s)
	freshAppeal, err := m.layout.db.Model().Appeal().GetAppealByID(ctx, appeal.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get fresh appeal data: %w", err)
	}

	session.AppealSelected.Set(s, freshAppeal)
	return freshAppeal, nil
}

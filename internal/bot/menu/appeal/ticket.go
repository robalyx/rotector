package appeal

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	builder "github.com/robalyx/rotector/internal/bot/builder/appeal"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/pagination"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/interfaces"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
	"go.uber.org/zap"
)

// TicketMenu handles the display and interaction logic for individual appeal tickets.
type TicketMenu struct {
	layout *Layout
	page   *pagination.Page
}

// NewTicketMenu creates a new ticket menu.
func NewTicketMenu(layout *Layout) *TicketMenu {
	m := &TicketMenu{layout: layout}
	m.page = &pagination.Page{
		Name: constants.AppealTicketPageName,
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return builder.NewTicketBuilder(s).Build()
		},
		CleanupHandlerFunc: m.Cleanup,
		ShowHandlerFunc:    m.Show,
		ButtonHandlerFunc:  m.handleButton,
		ModalHandlerFunc:   m.handleModal,
	}
	return m
}

// Show prepares and displays the appeal ticket interface.
func (m *TicketMenu) Show(event interfaces.CommonEvent, s *session.Session, r *pagination.Respond) {
	// Use fresh appeal data from database
	appeal, err := m.useFreshAppeal(s)
	if err != nil {
		r.Error(event, "Failed to use fresh appeal. Please try again.")
		return
	}

	// If appeal is pending, check if user's status has changed
	if appeal.Status == enum.AppealStatusPending { //nolint:nestif
		// Get current user status
		user, err := m.layout.db.Models().Users().GetUserByID(context.Background(), strconv.FormatUint(appeal.UserID, 10), types.UserFieldAll)
		if err != nil {
			if !errors.Is(err, types.ErrUserNotFound) {
				m.layout.logger.Error("Failed to get user status", zap.Error(err))
				r.Error(event, "Failed to verify user status. Please try again.")
				return
			}

			// User no longer exists, auto-reject the appeal
			if err := m.layout.db.Models().Appeals().RejectAppeal(context.Background(), appeal.ID, appeal.Timestamp, "User no longer exists in database."); err != nil {
				m.layout.logger.Error("Failed to auto-reject appeal", zap.Error(err))
			}

			ResetAppealData(s)
			r.Reload(event, s, "Appeal automatically closed: User no longer exists in database.")
			return
		}

		if user.Status != enum.UserTypeConfirmed && user.Status != enum.UserTypeFlagged {
			// User is no longer flagged or confirmed, auto-reject the appeal
			reason := fmt.Sprintf("User status changed to %s", user.Status)
			if err := m.layout.db.Models().Appeals().RejectAppeal(context.Background(), appeal.ID, appeal.Timestamp, reason); err != nil {
				m.layout.logger.Error("Failed to auto-reject appeal", zap.Error(err))
			}

			ResetAppealData(s)
			r.Reload(event, s, "Appeal automatically closed: User status changed to "+user.Status.String())
			return
		}
	}

	// Get messages for the appeal
	messages, err := m.layout.db.Models().Appeals().GetAppealMessages(context.Background(), appeal.ID)
	if err != nil {
		m.layout.logger.Error("Failed to get appeal messages", zap.Error(err))
		r.Error(event, "Failed to load appeal messages. Please try again.")
		return
	}

	// Calculate total pages
	totalPages := max((len(messages)-1)/constants.AppealMessagesPerPage, 0)

	// Store data in session
	session.AppealMessages.Set(s, messages)
	session.PaginationTotalPages.Set(s, totalPages)
	session.PaginationPage.Set(s, 0)
}

// Cleanup handles the cleanup of the appeal ticket interface.
func (m *TicketMenu) Cleanup(s *session.Session) {
	session.AppealMessages.Delete(s)
	session.PaginationTotalPages.Delete(s)
	session.PaginationPage.Delete(s)
}

// handleButton processes button interactions.
func (m *TicketMenu) handleButton(event *events.ComponentInteractionCreate, s *session.Session, r *pagination.Respond, customID string) {
	action := session.ViewerAction(customID)
	switch action {
	case session.ViewerFirstPage, session.ViewerPrevPage, session.ViewerNextPage, session.ViewerLastPage:
		messages := session.AppealMessages.Get(s)

		maxPage := (len(messages) - 1) / constants.AppealMessagesPerPage
		page := action.ParsePageAction(s, action, maxPage)

		session.PaginationPage.Set(s, page)
		r.Cancel(event, s, "")
		return
	}

	// Use fresh appeal data from database
	appeal, err := m.useFreshAppeal(s)
	if err != nil {
		r.Error(event, "Failed to use fresh appeal. Please try again.")
		return
	}

	switch customID {
	case constants.BackButtonCustomID:
		r.NavigateBack(event, s, "")
	case constants.AppealRespondButtonCustomID:
		m.handleRespond(event, r)
	case constants.AppealLookupUserButtonCustomID:
		m.handleLookupUser(event, s, r, appeal)
	case constants.AppealClaimButtonCustomID:
		m.handleClaimAppeal(event, s, r, appeal)
	case constants.AcceptAppealButtonCustomID:
		m.handleAcceptAppeal(event, r)
	case constants.RejectAppealButtonCustomID:
		m.handleRejectAppeal(event, r)
	case constants.AppealCloseButtonCustomID:
		m.handleCloseAppeal(event, s, r, appeal)
	}
}

// handleRespond opens a modal for responding to the appeal.
func (m *TicketMenu) handleRespond(event *events.ComponentInteractionCreate, r *pagination.Respond) {
	modal := discord.NewModalCreateBuilder().
		SetCustomID(constants.AppealRespondModalCustomID).
		SetTitle("Respond to Appeal").
		AddActionRow(
			discord.NewTextInput(constants.AppealReasonInputCustomID, discord.TextInputStyleParagraph, "Message").
				WithRequired(true).
				WithMaxLength(512).
				WithPlaceholder("Type your response..."),
		).
		Build()

	if err := event.Modal(modal); err != nil {
		m.layout.logger.Error("Failed to create response modal", zap.Error(err))
		r.Error(event, "Failed to open response modal. Please try again.")
	}
}

// handleLookupUser opens the review menu for the appealed user.
func (m *TicketMenu) handleLookupUser(event *events.ComponentInteractionCreate, s *session.Session, r *pagination.Respond, appeal *types.FullAppeal) {
	// Get user from database
	user, err := m.layout.db.Models().Users().GetUserByID(context.Background(), strconv.FormatUint(appeal.UserID, 10), types.UserFieldAll)
	if err != nil {
		if errors.Is(err, types.ErrUserNotFound) {
			r.Cancel(event, s, "Failed to find user. They may not be in our database.")
			return
		}
		m.layout.logger.Error("Failed to fetch user for review", zap.Error(err))
		r.Error(event, "Failed to fetch user for review. Please try again.")
		return
	}

	// Store user in session and show review menu
	session.UserTarget.Set(s, user)
	r.Show(event, s, constants.UserReviewPageName, "")

	// Log the lookup action
	m.layout.db.Models().Activities().Log(context.Background(), &types.ActivityLog{
		ActivityTarget: types.ActivityTarget{
			UserID: user.ID,
		},
		ReviewerID:        uint64(event.User().ID),
		ActivityType:      enum.ActivityTypeUserLookup,
		ActivityTimestamp: time.Now(),
		Details:           map[string]any{},
	})
}

// handleClaimAppeal handles claiming an appeal by a reviewer.
func (m *TicketMenu) handleClaimAppeal(event *events.ComponentInteractionCreate, s *session.Session, r *pagination.Respond, appeal *types.FullAppeal) {
	// Verify the appeal is not already claimed
	if appeal.ClaimedBy != 0 {
		r.Cancel(event, s, "This appeal is already claimed by another reviewer.")
		return
	}

	// Claim the appeal
	ctx := context.Background()
	reviewerID := uint64(event.User().ID)

	// Update the appeal in the database
	appeal.ClaimedBy = reviewerID
	appeal.ClaimedAt = time.Now()

	if err := m.layout.db.Models().Appeals().ClaimAppeal(ctx, appeal.ID, appeal.Timestamp, reviewerID); err != nil {
		m.layout.logger.Error("Failed to claim appeal", zap.Error(err))
		r.Error(event, "Failed to claim appeal. Please try again.")
		return
	}

	// Reload the appeal
	session.AppealSelected.Set(s, appeal)
	r.Reload(event, s, "Appeal claimed successfully.")

	// Log the claim action
	m.layout.db.Models().Activities().Log(ctx, &types.ActivityLog{
		ActivityTarget: types.ActivityTarget{
			UserID: appeal.UserID,
		},
		ReviewerID:        reviewerID,
		ActivityType:      enum.ActivityTypeAppealClaimed,
		ActivityTimestamp: time.Now(),
		Details: map[string]any{
			"appeal_id": appeal.ID,
		},
	})
}

// handleAcceptAppeal opens a modal for accepting the appeal with a reason.
func (m *TicketMenu) handleAcceptAppeal(event *events.ComponentInteractionCreate, r *pagination.Respond) {
	modal := discord.NewModalCreateBuilder().
		SetCustomID(constants.AcceptAppealModalCustomID).
		SetTitle("Accept Appeal").
		AddActionRow(
			discord.NewTextInput(constants.AppealReasonInputCustomID, discord.TextInputStyleParagraph, "Accept Reason").
				WithRequired(true).
				WithPlaceholder("Enter the reason for accepting this appeal..."),
		).
		Build()

	if err := event.Modal(modal); err != nil {
		m.layout.logger.Error("Failed to create accept modal", zap.Error(err))
		r.Error(event, "Failed to open accept modal. Please try again.")
	}
}

// handleRejectAppeal opens a modal for rejecting the appeal with a reason.
func (m *TicketMenu) handleRejectAppeal(event *events.ComponentInteractionCreate, r *pagination.Respond) {
	modal := discord.NewModalCreateBuilder().
		SetCustomID(constants.RejectAppealModalCustomID).
		SetTitle("Reject Appeal").
		AddActionRow(
			discord.NewTextInput(constants.AppealReasonInputCustomID, discord.TextInputStyleParagraph, "Reject Reason").
				WithRequired(true).
				WithPlaceholder("Enter the reason for rejecting this appeal..."),
		).
		Build()

	if err := event.Modal(modal); err != nil {
		m.layout.logger.Error("Failed to create reject modal", zap.Error(err))
		r.Error(event, "Failed to open reject modal. Please try again.")
	}
}

// handleCloseAppeal handles the user closing their own appeal ticket.
func (m *TicketMenu) handleCloseAppeal(event *events.ComponentInteractionCreate, s *session.Session, r *pagination.Respond, appeal *types.FullAppeal) {
	// Verify the user is the appeal creator
	userID := uint64(event.User().ID)
	if userID != appeal.RequesterID {
		r.Cancel(event, s, "Only the appeal creator can close this ticket.")
		return
	}

	// Close the appeal by rejecting it
	err := m.layout.db.Models().Appeals().RejectAppeal(context.Background(), appeal.ID, appeal.Timestamp, "Closed by appeal creator")
	if err != nil {
		m.layout.logger.Error("Failed to close appeal",
			zap.Error(err),
			zap.Int64("appealID", appeal.ID))
		r.Error(event, "Failed to close appeal. Please try again.")
		return
	}

	// Return to overview
	ResetAppealData(s)
	r.NavigateBack(event, s, "Appeal closed successfully.")

	// Log the appeal closing
	m.layout.db.Models().Activities().Log(context.Background(), &types.ActivityLog{
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

// handleModal processes modal submissions.
func (m *TicketMenu) handleModal(event *events.ModalSubmitInteractionCreate, s *session.Session, r *pagination.Respond) {
	// Use fresh appeal data from database
	appeal, err := m.useFreshAppeal(s)
	if err != nil {
		r.Error(event, "Failed to use fresh appeal. Please try again.")
		return
	}

	switch event.Data.CustomID {
	case constants.AppealRespondModalCustomID:
		m.handleRespondModalSubmit(event, s, r, appeal)
	case constants.AcceptAppealModalCustomID:
		m.handleAcceptModalSubmit(event, s, r, appeal)
	case constants.RejectAppealModalCustomID:
		m.handleRejectModalSubmit(event, s, r, appeal)
	}
}

// handleRespondModalSubmit processes the response modal submission.
func (m *TicketMenu) handleRespondModalSubmit(event *events.ModalSubmitInteractionCreate, s *session.Session, r *pagination.Respond, appeal *types.FullAppeal) {
	// Only allow responses for pending appeals
	if appeal.Status != enum.AppealStatusPending {
		r.Cancel(event, s, "Cannot respond to a closed appeal.")
		return
	}

	// Check if response is empty
	content := event.Data.Text(constants.AppealReasonInputCustomID)
	if content == "" {
		r.Cancel(event, s, "Response cannot be empty.")
		return
	}

	// Get user role and check rate limit
	userID := uint64(event.User().ID)
	role := enum.MessageRoleUser

	if s.BotSettings().IsReviewer(userID) {
		role = enum.MessageRoleModerator
	} else {
		// Check if user is allowed to send a message
		messages := session.AppealMessages.Get(s)
		if allowed, errorMsg := m.isMessageAllowed(messages, userID); !allowed {
			r.Cancel(event, s, errorMsg)
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
	err := m.layout.db.Models().Appeals().AddAppealMessage(context.Background(), message, appeal.ID, appeal.Timestamp)
	if err != nil {
		m.layout.logger.Error("Failed to add appeal message", zap.Error(err))
		r.Error(event, "Failed to save response. Please try again.")
		return
	}

	// Refresh the ticket view
	r.Reload(event, s, "Response added successfully.")
}

// handleAcceptModalSubmit processes the accept appeal submission.
func (m *TicketMenu) handleAcceptModalSubmit(event *events.ModalSubmitInteractionCreate, s *session.Session, r *pagination.Respond, appeal *types.FullAppeal) {
	// Prevent accepting already processed appeals
	if appeal.Status != enum.AppealStatusPending {
		r.Cancel(event, s, "This appeal has already been processed.")
		return
	}

	reason := event.Data.Text(constants.AppealReasonInputCustomID)
	if reason == "" {
		r.Cancel(event, s, "Accept reason cannot be empty.")
		return
	}

	// Get user to clear
	user, err := m.layout.db.Models().Users().GetUserByID(context.Background(), strconv.FormatUint(appeal.UserID, 10), types.UserFieldAll)
	if err != nil {
		if errors.Is(err, types.ErrUserNotFound) {
			r.Cancel(event, s, "Failed to find user. They may no longer exist in our database.")
			return
		}
		m.layout.logger.Error("Failed to get user for clearing", zap.Error(err))
		r.Error(event, "Failed to get user information. Please try again.")
		return
	}

	// Clear the user
	if user.Status != enum.UserTypeCleared {
		if err := m.layout.db.Models().Users().ClearUser(context.Background(), user); err != nil {
			m.layout.logger.Error("Failed to clear user", zap.Error(err))
			r.Error(event, "Failed to clear user. Please try again.")
			return
		}
	}

	// Accept the appeal
	err = m.layout.db.Models().Appeals().AcceptAppeal(context.Background(), appeal.ID, appeal.Timestamp, reason)
	if err != nil {
		m.layout.logger.Error("Failed to accept appeal", zap.Error(err))
		r.Error(event, "Failed to accept appeal. Please try again.")
		return
	}

	// Refresh the ticket view
	ResetAppealData(s)
	r.NavigateBack(event, s, "Appeal accepted and user cleared.")

	// Log the appeal acceptance
	m.layout.db.Models().Activities().Log(context.Background(), &types.ActivityLog{
		ActivityTarget: types.ActivityTarget{
			UserID: appeal.UserID,
		},
		ReviewerID:        uint64(event.User().ID),
		ActivityType:      enum.ActivityTypeAppealAccepted,
		ActivityTimestamp: time.Now(),
		Details: map[string]any{
			"reason":    reason,
			"appeal_id": appeal.ID,
		},
	})
}

// handleRejectModalSubmit processes the reject appeal submission.
func (m *TicketMenu) handleRejectModalSubmit(event *events.ModalSubmitInteractionCreate, s *session.Session, r *pagination.Respond, appeal *types.FullAppeal) {
	// Prevent rejecting already processed appeals
	if appeal.Status != enum.AppealStatusPending {
		r.Cancel(event, s, "This appeal has already been processed.")
		return
	}

	reason := event.Data.Text(constants.AppealReasonInputCustomID)
	if reason == "" {
		r.Cancel(event, s, "Reject reason cannot be empty.")
		return
	}

	// Reject the appeal
	err := m.layout.db.Models().Appeals().RejectAppeal(context.Background(), appeal.ID, appeal.Timestamp, reason)
	if err != nil {
		m.layout.logger.Error("Failed to reject appeal", zap.Error(err))
		r.Error(event, "Failed to reject appeal. Please try again.")
		return
	}

	// Refresh the ticket view
	ResetAppealData(s)
	r.NavigateBack(event, s, "Appeal rejected.")

	// Log the appeal rejection
	m.layout.db.Models().Activities().Log(context.Background(), &types.ActivityLog{
		ActivityTarget: types.ActivityTarget{
			UserID: appeal.UserID,
		},
		ReviewerID:        uint64(event.User().ID),
		ActivityType:      enum.ActivityTypeAppealRejected,
		ActivityTimestamp: time.Now(),
		Details: map[string]any{
			"reason":    reason,
			"appeal_id": appeal.ID,
		},
	})
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
func (m *TicketMenu) useFreshAppeal(s *session.Session) (*types.FullAppeal, error) {
	appeal := session.AppealSelected.Get(s)
	freshAppeal, err := m.layout.db.Models().Appeals().GetAppealByID(context.Background(), appeal.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get fresh appeal data: %w", err)
	}

	session.AppealSelected.Set(s, freshAppeal)
	return freshAppeal, nil
}

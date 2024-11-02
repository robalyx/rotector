package review

import (
	"context"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/rotector/rotector/internal/bot/constants"
	"github.com/rotector/rotector/internal/bot/handlers/review/builders"
	"github.com/rotector/rotector/internal/bot/interfaces"
	"github.com/rotector/rotector/internal/bot/pagination"
	"github.com/rotector/rotector/internal/bot/session"
	"github.com/rotector/rotector/internal/common/queue"
	"go.uber.org/zap"
)

// StatusMenu handles the recheck status menu.
type StatusMenu struct {
	handler *Handler
	page    *pagination.Page
}

// NewStatusMenu creates a new StatusMenu instance.
func NewStatusMenu(h *Handler) *StatusMenu {
	m := StatusMenu{handler: h}
	m.page = &pagination.Page{
		Name: "Status Menu",
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return builders.NewStatusEmbed(h.queueManager, s).Build()
		},
		ButtonHandlerFunc: m.handleButton,
	}
	return &m
}

// ShowStatusMenu displays the status menu.
func (m *StatusMenu) ShowStatusMenu(event interfaces.CommonEvent, s *session.Session) {
	// Update queue counts
	s.Set(constants.SessionKeyQueueHighCount, m.handler.queueManager.GetQueueLength(context.Background(), queue.HighPriority))
	s.Set(constants.SessionKeyQueueNormalCount, m.handler.queueManager.GetQueueLength(context.Background(), queue.NormalPriority))
	s.Set(constants.SessionKeyQueueLowCount, m.handler.queueManager.GetQueueLength(context.Background(), queue.LowPriority))

	// Check if processing is complete
	userID := s.GetUint64(constants.SessionKeyQueueUser)
	status, _, _, err := m.handler.queueManager.GetQueueInfo(context.Background(), userID)
	if err == nil && (status == queue.StatusComplete || status == queue.StatusSkipped) {
		// Clear queue info
		if err := m.handler.queueManager.ClearQueueInfo(context.Background(), userID); err != nil {
			m.handler.logger.Error("Failed to clear queue info",
				zap.Error(err),
				zap.Uint64("userID", userID))
		}

		// Try to get the flagged user from the database
		flaggedUser, err := m.handler.db.Users().GetFlaggedUserByID(userID)
		if err != nil {
			// User was not flagged by AI, show new user
			m.returnToPreviousPage(event, s, "User was not flagged by AI after recheck.")
			return
		}

		// User is still flagged, show updated user
		s.Set(constants.SessionKeyTarget, flaggedUser)
		m.handler.reviewMenu.ShowReviewMenu(event, s, "User has been rechecked. Showing updated information.")
		return
	}

	m.handler.paginationManager.NavigateTo(event, s, m.page, "")
}

// handleButton handles button interactions.
func (m *StatusMenu) handleButton(event *events.ComponentInteractionCreate, s *session.Session, customID string) {
	switch customID {
	case constants.RefreshButtonCustomID:
		m.ShowStatusMenu(event, s)
	case constants.AbortButtonCustomID:
		m.handleAbort(event, s)
	}
}

// handleAbort handles the abort button interaction.
func (m *StatusMenu) handleAbort(event *events.ComponentInteractionCreate, s *session.Session) {
	userID := s.GetUint64(constants.SessionKeyQueueUser)

	// Mark as aborted (will be cleaned up after 24 hours)
	if err := m.handler.queueManager.MarkAsAborted(context.Background(), userID); err != nil {
		m.handler.paginationManager.RespondWithError(event, "Failed to mark user as aborted")
		return
	}

	// Clear queue info
	if err := m.handler.queueManager.ClearQueueInfo(context.Background(), userID); err != nil {
		m.handler.paginationManager.RespondWithError(event, "Failed to clear queue info")
		return
	}

	// Note: We don't need to explicitly remove from queue here
	// The worker will handle that when it sees the aborted flag

	m.returnToPreviousPage(event, s, "Recheck aborted")
}

// returnToPreviousPage returns to the previous page the user was on.
func (m *StatusMenu) returnToPreviousPage(event interfaces.CommonEvent, s *session.Session, content string) {
	previousPage := s.GetString(constants.SessionKeyPreviousPage)
	page := m.handler.paginationManager.GetPage(previousPage)
	m.handler.paginationManager.NavigateTo(event, s, page, content)
}

package review

import (
	"time"

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
	// Check for timeout first
	if m.checkReviewTimeout(event, s) {
		return
	}

	// Update queue counts
	s.Set(constants.SessionKeyQueueHighCount, m.handler.queueManager.GetQueueLength(queue.HighPriority))
	s.Set(constants.SessionKeyQueueNormalCount, m.handler.queueManager.GetQueueLength(queue.NormalPriority))
	s.Set(constants.SessionKeyQueueLowCount, m.handler.queueManager.GetQueueLength(queue.LowPriority))

	// Check if processing is complete
	userID := s.GetUint64(constants.SessionKeyQueueUser)
	status, _, _, err := m.handler.queueManager.GetQueueInfo(userID)
	if err == nil && (status == queue.StatusComplete || status == queue.StatusSkipped) {
		// Try to get the flagged user from the database
		flaggedUser, err := m.handler.db.Users().GetFlaggedUserByID(userID)
		if err != nil {
			// User was not flagged by AI, show new user
			m.handler.reviewMenu.ShowReviewMenuAndFetchUser(event, s,
				"Previous user was not flagged by AI after recheck. Showing new user.")
			return
		}

		// Clear queue info
		if err := m.handler.queueManager.ClearQueueInfo(flaggedUser.ID); err != nil {
			m.handler.logger.Error("Failed to clear queue info",
				zap.Error(err),
				zap.Uint64("userID", flaggedUser.ID))
		}

		// User is still flagged, show updated user
		s.Set(constants.SessionKeyTarget, flaggedUser)
		m.handler.reviewMenu.ShowReviewMenu(event, s,
			"User has been rechecked. Showing updated information.")
		return
	}

	m.handler.paginationManager.NavigateTo(event, s, m.page, "")
}

// handleButton handles button interactions.
func (m *StatusMenu) handleButton(event *events.ComponentInteractionCreate, s *session.Session, customID string) {
	// Check for timeout first
	if m.checkReviewTimeout(event, s) {
		return
	}

	switch customID {
	case constants.BackButtonCustomID:
		m.handler.reviewMenu.ShowReviewMenuAndFetchUser(event, s, "The previous user was queued. Showing new user.")
	case constants.RefreshButtonCustomID:
		m.ShowStatusMenu(event, s)
	case constants.AbortButtonCustomID:
		m.handleAbort(event, s)
	}
}

// handleAbort handles the abort button interaction.
func (m *StatusMenu) handleAbort(event *events.ComponentInteractionCreate, s *session.Session) {
	user := s.GetFlaggedUser(constants.SessionKeyTarget)

	// Mark as aborted (will be cleaned up after 24 hours)
	if err := m.handler.queueManager.MarkAsAborted(user.ID); err != nil {
		m.handler.paginationManager.RespondWithError(event, "Failed to mark user as aborted")
		return
	}

	// Clear queue info
	if err := m.handler.queueManager.ClearQueueInfo(user.ID); err != nil {
		m.handler.paginationManager.RespondWithError(event, "Failed to clear queue info")
		return
	}

	// Note: We don't need to explicitly remove from queue here
	// The worker will handle that when it sees the aborted flag

	m.handler.reviewMenu.ShowReviewMenu(event, s, "Recheck aborted")
}

// checkReviewTimeout checks if the review session has expired.
func (m *StatusMenu) checkReviewTimeout(event interfaces.CommonEvent, s *session.Session) bool {
	user := s.GetFlaggedUser(constants.SessionKeyTarget)
	if time.Since(user.LastViewed) > 10*time.Minute {
		m.handler.reviewMenu.ShowReviewMenuAndFetchUser(event, s, "Previous review session expired (10 minutes). Showing new user.")
		return true
	}
	return false
}

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

// StatusMenu handles the display and interaction logic for viewing queue status.
// It works with the status builder to show queue position and processing progress.
type StatusMenu struct {
	handler *Handler
	page    *pagination.Page
}

// NewStatusMenu creates a StatusMenu and sets up its page with message builders
// and interaction handlers. The page is configured to show queue information
// and handle refresh/abort actions.
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

// ShowStatusMenu prepares and displays the status interface by loading
// current queue counts and position information into the session.
func (m *StatusMenu) ShowStatusMenu(event interfaces.CommonEvent, s *session.Session) {
	// Update queue counts for each priority level
	s.Set(constants.SessionKeyQueueHighCount, m.handler.queueManager.GetQueueLength(context.Background(), queue.HighPriority))
	s.Set(constants.SessionKeyQueueNormalCount, m.handler.queueManager.GetQueueLength(context.Background(), queue.NormalPriority))
	s.Set(constants.SessionKeyQueueLowCount, m.handler.queueManager.GetQueueLength(context.Background(), queue.LowPriority))

	// Check if processing is complete
	userID := s.GetUint64(constants.SessionKeyQueueUser)
	status, _, _, err := m.handler.queueManager.GetQueueInfo(context.Background(), userID)
	if err == nil && (status == queue.StatusComplete || status == queue.StatusSkipped) {
		// Clean up queue info
		if err := m.handler.queueManager.ClearQueueInfo(context.Background(), userID); err != nil {
			m.handler.logger.Error("Failed to clear queue info",
				zap.Error(err),
				zap.Uint64("userID", userID))
		}

		// Check if user was flagged after recheck
		flaggedUser, err := m.handler.db.Users().GetFlaggedUserByID(userID)
		if err != nil {
			// User was not flagged by AI, return to previous page
			m.returnToPreviousPage(event, s, "User was not flagged by AI after recheck.")
			return
		}

		// User is still flagged, show updated information
		s.Set(constants.SessionKeyTarget, flaggedUser)
		m.handler.reviewMenu.ShowReviewMenu(event, s, "User has been rechecked. Showing updated information.")
		return
	}

	m.handler.paginationManager.NavigateTo(event, s, m.page, "")
}

// handleButton processes refresh and abort button clicks.
func (m *StatusMenu) handleButton(event *events.ComponentInteractionCreate, s *session.Session, customID string) {
	switch customID {
	case constants.RefreshButtonCustomID:
		m.ShowStatusMenu(event, s)
	case constants.AbortButtonCustomID:
		m.handleAbort(event, s)
	}
}

// handleAbort marks a queued user as aborted and cleans up queue information.
// The worker will handle removing the user from the queue when it sees the abort flag.
func (m *StatusMenu) handleAbort(event *events.ComponentInteractionCreate, s *session.Session) {
	userID := s.GetUint64(constants.SessionKeyQueueUser)

	// Mark as aborted (will be cleaned up after 24 hours)
	if err := m.handler.queueManager.MarkAsAborted(context.Background(), userID); err != nil {
		m.handler.paginationManager.RespondWithError(event, "Failed to mark user as aborted")
		return
	}

	// Clean up queue info
	if err := m.handler.queueManager.ClearQueueInfo(context.Background(), userID); err != nil {
		m.handler.paginationManager.RespondWithError(event, "Failed to clear queue info")
		return
	}

	m.returnToPreviousPage(event, s, "Recheck aborted")
}

// returnToPreviousPage navigates back to the page stored in session history.
func (m *StatusMenu) returnToPreviousPage(event interfaces.CommonEvent, s *session.Session, content string) {
	previousPage := s.GetString(constants.SessionKeyPreviousPage)
	page := m.handler.paginationManager.GetPage(previousPage)
	m.handler.paginationManager.NavigateTo(event, s, page, content)
}

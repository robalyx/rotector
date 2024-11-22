package user

import (
	"context"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	builder "github.com/rotector/rotector/internal/bot/builder/review/user"
	"github.com/rotector/rotector/internal/bot/constants"
	"github.com/rotector/rotector/internal/bot/core/pagination"
	"github.com/rotector/rotector/internal/bot/core/session"
	"github.com/rotector/rotector/internal/bot/interfaces"
	"github.com/rotector/rotector/internal/common/queue"
	"github.com/rotector/rotector/internal/common/storage/database/models"
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
			return builder.NewStatusBuilder(h.queueManager, s).Build()
		},
		ButtonHandlerFunc: m.handleButton,
	}
	return &m
}

// ShowStatusMenu prepares and displays the status interface by loading
// current queue counts and position information into the session.
func (m *StatusMenu) ShowStatusMenu(event interfaces.CommonEvent, s *session.Session) {
	userID := s.GetUint64(constants.SessionKeyQueueUser)
	status, priority, position, err := m.handler.queueManager.GetQueueInfo(context.Background(), userID)

	// Check if processing is complete
	if err == nil && (status == queue.StatusComplete || status == queue.StatusSkipped) {
		// Check if user was flagged after recheck
		flaggedUser, err := m.handler.db.Users().GetFlaggedUserByIDToReview(context.Background(), userID)
		if err != nil {
			// User was not flagged by AI, return to previous page
			m.handler.paginationManager.NavigateBack(event, s, "User was not flagged by AI after recheck.")
			return
		}

		// User is still flagged, show updated information
		s.Set(constants.SessionKeyTarget, flaggedUser)

		// Log the view action asynchronously
		go m.handler.db.UserActivity().LogActivity(context.Background(), &models.UserActivityLog{
			ActivityTarget: models.ActivityTarget{
				UserID: flaggedUser.ID,
			},
			ReviewerID:        uint64(event.User().ID),
			ActivityType:      models.ActivityTypeUserViewed,
			ActivityTimestamp: time.Now(),
			Details:           make(map[string]interface{}),
		})

		m.handler.reviewMenu.ShowReviewMenu(event, s, "User has been rechecked. Showing updated information.")
		return
	}

	// Update queue counts for each priority level
	s.Set(constants.SessionKeyQueueHighCount, m.handler.queueManager.GetQueueLength(context.Background(), queue.HighPriority))
	s.Set(constants.SessionKeyQueueNormalCount, m.handler.queueManager.GetQueueLength(context.Background(), queue.NormalPriority))
	s.Set(constants.SessionKeyQueueLowCount, m.handler.queueManager.GetQueueLength(context.Background(), queue.LowPriority))

	// Update queue information
	s.Set(constants.SessionKeyQueueStatus, status)
	s.Set(constants.SessionKeyQueuePriority, priority)
	s.Set(constants.SessionKeyQueuePosition, position)

	m.handler.paginationManager.NavigateTo(event, s, m.page, "")
}

// handleButton processes refresh and abort button clicks.
func (m *StatusMenu) handleButton(event *events.ComponentInteractionCreate, s *session.Session, customID string) {
	switch customID {
	case constants.RefreshButtonCustomID:
		m.ShowStatusMenu(event, s)
	case constants.AbortButtonCustomID:
		m.handler.paginationManager.NavigateBack(event, s, "Recheck aborted")
	}
}

package user

import (
	"context"
	"strconv"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	builder "github.com/robalyx/rotector/internal/bot/builder/review/user"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/pagination"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/interfaces"
	"github.com/robalyx/rotector/internal/common/queue"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
)

// StatusMenu handles the display and interaction logic for viewing queue status.
type StatusMenu struct {
	layout *Layout
	page   *pagination.Page
}

// NewStatusMenu creates a StatusMenu and sets up its page with message builders
// and interaction handlers. The page is configured to show queue information
// and handle refresh/abort actions.
func NewStatusMenu(layout *Layout) *StatusMenu {
	m := &StatusMenu{layout: layout}
	m.page = &pagination.Page{
		Name: constants.UserStatusPageName,
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return builder.NewStatusBuilder(layout.queueManager, s).Build()
		},
		ButtonHandlerFunc: m.handleButton,
	}
	return m
}

// Show prepares and displays the status interface by loading
// current queue counts and position information into the session.
func (m *StatusMenu) Show(event interfaces.CommonEvent, s *session.Session) {
	userID := session.QueueUser.Get(s)
	status, priority, position, err := m.layout.queueManager.GetQueueInfo(context.Background(), userID)

	// Check if processing is complete
	if err == nil && (status == queue.StatusComplete || status == queue.StatusSkipped) {
		// Check if user was flagged after recheck
		user, err := m.layout.db.Models().Users().GetUserByID(context.Background(), strconv.FormatUint(userID, 10), types.UserFields{})
		if err != nil {
			// User was not flagged by AI, return to previous page
			m.layout.paginationManager.NavigateBack(event, s, "User was not flagged by AI after recheck.")
			return
		}

		// User is still flagged, show updated information
		dashboardPage := m.layout.paginationManager.GetPage(constants.DashboardPageName)
		m.layout.paginationManager.UpdatePage(s, dashboardPage)

		session.UserTarget.Set(s, user)
		m.layout.reviewMenu.Show(event, s, "User has been rechecked. Showing updated information.")

		// Log the view action
		m.layout.db.Models().Activities().Log(context.Background(), &types.ActivityLog{
			ActivityTarget: types.ActivityTarget{
				UserID: user.ID,
			},
			ReviewerID:        uint64(event.User().ID),
			ActivityType:      enum.ActivityTypeUserViewed,
			ActivityTimestamp: time.Now(),
			Details:           map[string]interface{}{},
		})
		return
	}

	// Update queue counts for each priority level
	session.QueueHighCount.Set(s, m.layout.queueManager.GetQueueLength(context.Background(), queue.HighPriority))
	session.QueueNormalCount.Set(s, m.layout.queueManager.GetQueueLength(context.Background(), queue.NormalPriority))
	session.QueueLowCount.Set(s, m.layout.queueManager.GetQueueLength(context.Background(), queue.LowPriority))

	// Update queue information
	session.QueueStatus.Set(s, status)
	session.QueuePriority.Set(s, priority)
	session.QueuePosition.Set(s, position)

	m.layout.paginationManager.NavigateTo(event, s, m.page, "")
}

// handleButton processes refresh and abort button clicks.
func (m *StatusMenu) handleButton(event *events.ComponentInteractionCreate, s *session.Session, customID string) {
	switch customID {
	case constants.RefreshButtonCustomID:
		m.Show(event, s)
	case constants.AbortButtonCustomID:
		m.layout.paginationManager.NavigateBack(event, s, "Recheck aborted")
	}
}

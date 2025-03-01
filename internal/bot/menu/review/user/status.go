package user

import (
	"context"
	"errors"
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
	"go.uber.org/zap"
)

// StatusMenu handles the display and interaction logic for viewing queue status.
type StatusMenu struct {
	layout *Layout
	page   *pagination.Page
}

// NewStatusMenu creates a new status menu.
func NewStatusMenu(layout *Layout) *StatusMenu {
	m := &StatusMenu{layout: layout}
	m.page = &pagination.Page{
		Name: constants.UserStatusPageName,
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return builder.NewStatusBuilder(layout.queueManager, s).Build()
		},
		ShowHandlerFunc:   m.Show,
		ButtonHandlerFunc: m.handleButton,
	}
	return m
}

// Show prepares and displays the status interface.
func (m *StatusMenu) Show(event interfaces.CommonEvent, s *session.Session, r *pagination.Respond) {
	userID := session.QueueUser.Get(s)
	status, priority, position, err := m.layout.queueManager.GetQueueInfo(context.Background(), userID)

	// Check if processing is complete
	if err == nil && status == queue.StatusComplete {
		// Check if user was flagged after recheck
		user, err := m.layout.db.Models().Users().GetUserByID(context.Background(), strconv.FormatUint(userID, 10), types.UserFieldAll)
		if err != nil {
			if errors.Is(err, types.ErrUserNotFound) {
				r.NavigateBack(event, s, "User was not flagged by AI after recheck.")
				return
			}
			m.layout.logger.Error("Failed to get user by ID", zap.Error(err))
			return
		}

		// User is still flagged, show updated information
		r.UpdatePage(s, constants.UserReviewPageName)

		session.UserTarget.Set(s, user)
		r.Show(event, s, constants.UserReviewPageName, "User has been rechecked. Showing updated information.")

		// Log the view action
		m.layout.db.Models().Activities().Log(context.Background(), &types.ActivityLog{
			ActivityTarget: types.ActivityTarget{
				UserID: user.ID,
			},
			ReviewerID:        uint64(event.User().ID),
			ActivityType:      enum.ActivityTypeUserViewed,
			ActivityTimestamp: time.Now(),
			Details:           map[string]any{},
		})
		return
	}

	// Update queue counts for each priority level
	session.QueueHighCount.Set(s, m.layout.queueManager.GetQueueLength(context.Background(), queue.PriorityHigh))
	session.QueueNormalCount.Set(s, m.layout.queueManager.GetQueueLength(context.Background(), queue.PriorityNormal))
	session.QueueLowCount.Set(s, m.layout.queueManager.GetQueueLength(context.Background(), queue.PriorityLow))

	// Update queue information
	session.QueueStatus.Set(s, status)
	session.QueuePriority.Set(s, priority)
	session.QueuePosition.Set(s, position)
}

// handleButton processes refresh and abort button clicks.
func (m *StatusMenu) handleButton(event *events.ComponentInteractionCreate, s *session.Session, r *pagination.Respond, customID string) {
	switch customID {
	case constants.RefreshButtonCustomID:
		r.Reload(event, s, "")
	case constants.AbortButtonCustomID:
		r.NavigateBack(event, s, "Recheck aborted")
	}
}

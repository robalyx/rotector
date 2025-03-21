package user

import (
	"errors"
	"strconv"
	"time"

	"github.com/disgoorg/disgo/discord"
	builder "github.com/robalyx/rotector/internal/bot/builder/review/user"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/interaction"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/common/queue"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
	"go.uber.org/zap"
)

// StatusMenu handles the display and interaction logic for viewing queue status.
type StatusMenu struct {
	layout *Layout
	page   *interaction.Page
}

// NewStatusMenu creates a new status menu.
func NewStatusMenu(layout *Layout) *StatusMenu {
	m := &StatusMenu{layout: layout}
	m.page = &interaction.Page{
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
func (m *StatusMenu) Show(ctx *interaction.Context, s *session.Session) {
	userID := session.QueueUser.Get(s)
	status, priority, position, err := m.layout.queueManager.GetQueueInfo(ctx.Context(), userID)

	// Check if processing is complete
	if err == nil && status == queue.StatusComplete {
		// Check if user was flagged after recheck
		user, err := m.layout.db.Service().User().GetUserByID(
			ctx.Context(), strconv.FormatUint(userID, 10), types.UserFieldAll,
		)
		if err != nil {
			if errors.Is(err, types.ErrUserNotFound) {
				ctx.NavigateBack("User was not flagged by AI after recheck.")
				return
			}
			m.layout.logger.Error("Failed to get user by ID", zap.Error(err))
			return
		}

		// WORKAROUND:
		// Update the current page to dashboard so that when user navigates back,
		// they return to the dashboard instead of this status page
		ctx.UpdatePage(constants.DashboardPageName)

		// User is flagged, show updated information
		session.UserTarget.Set(s, user)
		ctx.Show(constants.UserReviewPageName, "User has been rechecked. Showing updated information.")

		// Log the view action
		m.layout.db.Model().Activity().Log(ctx.Context(), &types.ActivityLog{
			ActivityTarget: types.ActivityTarget{
				UserID: user.ID,
			},
			ReviewerID:        uint64(ctx.Event().User().ID),
			ActivityType:      enum.ActivityTypeUserViewed,
			ActivityTimestamp: time.Now(),
			Details:           map[string]any{},
		})
		return
	}

	// Update queue counts for each priority level
	session.QueueHighCount.Set(s, m.layout.queueManager.GetQueueLength(ctx.Context(), queue.PriorityHigh))
	session.QueueNormalCount.Set(s, m.layout.queueManager.GetQueueLength(ctx.Context(), queue.PriorityNormal))
	session.QueueLowCount.Set(s, m.layout.queueManager.GetQueueLength(ctx.Context(), queue.PriorityLow))

	// Update queue information
	session.QueueStatus.Set(s, status)
	session.QueuePriority.Set(s, priority)
	session.QueuePosition.Set(s, position)
}

// handleButton processes refresh and abort button clicks.
func (m *StatusMenu) handleButton(ctx *interaction.Context, _ *session.Session, customID string) {
	switch customID {
	case constants.RefreshButtonCustomID:
		ctx.Reload("")
	case constants.AbortButtonCustomID:
		ctx.NavigateBack("Recheck aborted")
	}
}

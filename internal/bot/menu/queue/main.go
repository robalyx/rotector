package queue

import (
	"strconv"
	"time"

	"github.com/disgoorg/disgo/discord"
	builder "github.com/robalyx/rotector/internal/bot/builder/queue"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/interaction"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/common/queue"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
	"github.com/robalyx/rotector/internal/common/utils"
	"go.uber.org/zap"
)

// Menu handles the display and interaction logic for queue management.
type Menu struct {
	layout *Layout
	page   *interaction.Page
}

// NewMenu creates a new queue menu.
func NewMenu(layout *Layout) *Menu {
	m := &Menu{layout: layout}
	m.page = &interaction.Page{
		Name: constants.QueuePageName,
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return builder.NewBuilder(s).Build()
		},
		ShowHandlerFunc:   m.Show,
		SelectHandlerFunc: m.handleSelectMenu,
		ButtonHandlerFunc: m.handleButton,
		ModalHandlerFunc:  m.handleModal,
	}
	return m
}

// Show prepares and displays the queue interface.
func (m *Menu) Show(ctx *interaction.Context, s *session.Session) {
	session.QueueHighCount.Set(s, m.layout.queueManager.GetQueueLength(ctx.Context(), queue.PriorityHigh))
	session.QueueNormalCount.Set(s, m.layout.queueManager.GetQueueLength(ctx.Context(), queue.PriorityNormal))
	session.QueueLowCount.Set(s, m.layout.queueManager.GetQueueLength(ctx.Context(), queue.PriorityLow))
}

// handleSelectMenu processes select menu interactions.
func (m *Menu) handleSelectMenu(ctx *interaction.Context, _ *session.Session, customID, option string) {
	if customID != constants.ActionSelectMenuCustomID {
		return
	}

	// Create modal for user ID and reason input
	modal := discord.NewModalCreateBuilder().
		SetCustomID(option).
		SetTitle("Add User to Queue").
		AddActionRow(
			discord.NewTextInput(constants.UserIDInputCustomID, discord.TextInputStyleShort, "User ID").
				WithRequired(true).
				WithPlaceholder("Enter the user ID"),
		).
		AddActionRow(
			discord.NewTextInput(constants.ReasonInputCustomID, discord.TextInputStyleParagraph, "Reason").
				WithRequired(true).
				WithPlaceholder("Enter the reason for adding this user"),
		)

	ctx.Modal(modal)
}

// handleButton processes button interactions.
func (m *Menu) handleButton(ctx *interaction.Context, _ *session.Session, customID string) {
	switch customID {
	case constants.BackButtonCustomID:
		ctx.NavigateBack("")
	case constants.RefreshButtonCustomID:
		ctx.Reload("")
	}
}

// handleModal processes modal submissions.
func (m *Menu) handleModal(ctx *interaction.Context, s *session.Session) {
	data := ctx.Event().ModalData()
	userIDStr := data.Text(constants.UserIDInputCustomID)
	reason := data.Text(constants.ReasonInputCustomID)

	// Parse profile URL if provided
	parsedURL, err := utils.ExtractUserIDFromURL(userIDStr)
	if err == nil {
		userIDStr = parsedURL
	}

	// Parse the user ID
	userID, err := strconv.ParseUint(userIDStr, 10, 64)
	if err != nil {
		ctx.Error("Invalid user ID format.")
		return
	}

	// Store user ID for status tracking
	session.QueueUser.Set(s, userID)

	// Check if user is already in queue
	status, _, _, err := m.layout.queueManager.GetQueueInfo(ctx.Context(), userID)
	if err == nil && status != "" {
		// Show status menu if already queued
		ctx.Show(constants.UserStatusPageName, "")
		return
	}

	// Convert custom ID to priority
	priority := queue.PriorityNormal
	switch ctx.Event().CustomID() {
	case constants.QueueHighPriorityCustomID:
		priority = queue.PriorityHigh
	case constants.QueueNormalPriorityCustomID:
		priority = queue.PriorityNormal
	case constants.QueueLowPriorityCustomID:
		priority = queue.PriorityLow
	}

	// Add to queue with selected priority
	err = m.layout.queueManager.AddToQueue(ctx.Context(), &queue.Item{
		UserID:   userID,
		Priority: priority,
		Reason:   reason,
		AddedBy:  uint64(ctx.Event().User().ID),
		AddedAt:  time.Now(),
		Status:   queue.StatusPending,
	})
	if err != nil {
		m.layout.logger.Error("Failed to add user to queue", zap.Error(err))
		ctx.Error("Failed to add user to queue")
		return
	}

	// Update queue info with position
	err = m.layout.queueManager.SetQueueInfo(
		ctx.Context(),
		userID,
		queue.StatusPending,
		queue.PriorityHigh,
		m.layout.queueManager.GetQueueLength(ctx.Context(), queue.PriorityHigh),
	)
	if err != nil {
		m.layout.logger.Error("Failed to update queue info", zap.Error(err))
		ctx.Error("Failed to update queue info")
		return
	}

	// Show status menu to track progress
	ctx.Show(constants.UserStatusPageName, "")

	// Log the activity
	m.layout.db.Model().Activity().Log(ctx.Context(), &types.ActivityLog{
		ActivityTarget: types.ActivityTarget{
			UserID: userID,
		},
		ReviewerID:        uint64(ctx.Event().User().ID),
		ActivityType:      enum.ActivityTypeUserRechecked,
		ActivityTimestamp: time.Now(),
		Details:           map[string]any{"reason": reason},
	})
}

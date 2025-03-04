package queue

import (
	"context"
	"strconv"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	builder "github.com/robalyx/rotector/internal/bot/builder/queue"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/pagination"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/interfaces"
	"github.com/robalyx/rotector/internal/common/queue"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
	"go.uber.org/zap"
)

// Menu handles the display and interaction logic for queue management.
type Menu struct {
	layout *Layout
	page   *pagination.Page
}

// NewMenu creates a new queue menu.
func NewMenu(layout *Layout) *Menu {
	m := &Menu{layout: layout}
	m.page = &pagination.Page{
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
func (m *Menu) Show(_ interfaces.CommonEvent, s *session.Session, _ *pagination.Respond) {
	session.QueueHighCount.Set(s, m.layout.queueManager.GetQueueLength(context.Background(), queue.PriorityHigh))
	session.QueueNormalCount.Set(s, m.layout.queueManager.GetQueueLength(context.Background(), queue.PriorityNormal))
	session.QueueLowCount.Set(s, m.layout.queueManager.GetQueueLength(context.Background(), queue.PriorityLow))
}

// handleSelectMenu processes select menu interactions.
func (m *Menu) handleSelectMenu(
	event *events.ComponentInteractionCreate, _ *session.Session, r *pagination.Respond, customID, option string,
) {
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
		).
		Build()

	if err := event.Modal(modal); err != nil {
		m.layout.logger.Error("Failed to show modal", zap.Error(err))
		r.Error(event, "Failed to show modal")
	}
}

// handleButton processes button interactions.
func (m *Menu) handleButton(
	event *events.ComponentInteractionCreate, s *session.Session, r *pagination.Respond, customID string,
) {
	switch customID {
	case constants.BackButtonCustomID:
		r.NavigateBack(event, s, "")
	case constants.RefreshButtonCustomID:
		r.Reload(event, s, "")
	}
}

// handleModal processes modal submissions.
func (m *Menu) handleModal(event *events.ModalSubmitInteractionCreate, s *session.Session, r *pagination.Respond) {
	// Parse user ID and get reason from modal
	userIDStr := event.Data.Text(constants.UserIDInputCustomID)
	reason := event.Data.Text(constants.ReasonInputCustomID)

	userID, err := strconv.ParseUint(userIDStr, 10, 64)
	if err != nil {
		r.Cancel(event, s, "Invalid user ID format.")
		return
	}

	// Store user ID for status tracking
	session.QueueUser.Set(s, userID)

	// Check if user is already in queue
	status, _, _, err := m.layout.queueManager.GetQueueInfo(context.Background(), userID)
	if err == nil && status != "" {
		// Show status menu if already queued
		r.Show(event, s, constants.StatusPageName, "")
		return
	}

	// Convert custom ID to priority
	priority := queue.PriorityNormal
	switch event.Data.CustomID {
	case constants.QueueHighPriorityCustomID:
		priority = queue.PriorityHigh
	case constants.QueueNormalPriorityCustomID:
		priority = queue.PriorityNormal
	case constants.QueueLowPriorityCustomID:
		priority = queue.PriorityLow
	}

	// Add to queue with selected priority
	err = m.layout.queueManager.AddToQueue(context.Background(), &queue.Item{
		UserID:      userID,
		Priority:    priority,
		Reason:      reason,
		AddedBy:     uint64(event.User().ID),
		AddedAt:     time.Now(),
		Status:      queue.StatusPending,
		CheckExists: false,
	})
	if err != nil {
		m.layout.logger.Error("Failed to add user to queue", zap.Error(err))
		r.Error(event, "Failed to add user to queue")
		return
	}

	// Update queue info with position
	err = m.layout.queueManager.SetQueueInfo(
		context.Background(),
		userID,
		queue.StatusPending,
		queue.PriorityHigh,
		m.layout.queueManager.GetQueueLength(context.Background(), queue.PriorityHigh),
	)
	if err != nil {
		m.layout.logger.Error("Failed to update queue info", zap.Error(err))
		r.Error(event, "Failed to update queue info")
		return
	}

	// Show status menu to track progress
	r.Show(event, s, constants.StatusPageName, "")

	// Log the activity
	m.layout.db.Models().Activities().Log(context.Background(), &types.ActivityLog{
		ActivityTarget: types.ActivityTarget{
			UserID: userID,
		},
		ReviewerID:        uint64(event.User().ID),
		ActivityType:      enum.ActivityTypeUserRechecked,
		ActivityTimestamp: time.Now(),
		Details:           map[string]any{"reason": reason},
	})
}

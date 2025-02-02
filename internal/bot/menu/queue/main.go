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

// MainMenu handles the display and interaction logic for queue management.
type MainMenu struct {
	layout *Layout
	page   *pagination.Page
}

// NewMainMenu creates a MainMenu and sets up its page with message builders and
// interaction handlers. The page is configured to show queue statistics
// and handle queue operations.
func NewMainMenu(layout *Layout) *MainMenu {
	m := &MainMenu{layout: layout}
	m.page = &pagination.Page{
		Name: constants.QueuePageName,
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			// Load current queue lengths for display
			highCount := session.QueueHighCount.Get(s)
			normalCount := session.QueueNormalCount.Get(s)
			lowCount := session.QueueLowCount.Get(s)

			return builder.NewBuilder(highCount, normalCount, lowCount).Build()
		},
		SelectHandlerFunc: m.handleSelectMenu,
		ButtonHandlerFunc: m.handleButton,
		ModalHandlerFunc:  m.handleModal,
	}
	return m
}

// Show prepares and displays the queue interface by loading
// current queue lengths into the session.
func (m *MainMenu) Show(event interfaces.CommonEvent, s *session.Session, content string) {
	// Store current queue lengths in session for the message builder
	session.QueueHighCount.Set(s, m.layout.queueManager.GetQueueLength(context.Background(), queue.PriorityHigh))
	session.QueueNormalCount.Set(s, m.layout.queueManager.GetQueueLength(context.Background(), queue.PriorityNormal))
	session.QueueLowCount.Set(s, m.layout.queueManager.GetQueueLength(context.Background(), queue.PriorityLow))

	m.layout.paginationManager.NavigateTo(event, s, m.page, content)
}

// handleSelectMenu processes select menu interactions by storing the selected
// priority and showing a modal for user ID input.
func (m *MainMenu) handleSelectMenu(event *events.ComponentInteractionCreate, _ *session.Session, customID string, option string) {
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
	}
}

// handleButton processes button interactions, mainly handling navigation
// back to the dashboard and queue refresh requests.
func (m *MainMenu) handleButton(event *events.ComponentInteractionCreate, s *session.Session, customID string) {
	switch customID {
	case constants.BackButtonCustomID:
		m.layout.paginationManager.NavigateBack(event, s, "")
	case constants.RefreshButtonCustomID:
		m.Show(event, s, "")
	}
}

// handleModal processes modal submissions by adding the user to the queue
// with the specified priority and reason.
func (m *MainMenu) handleModal(event *events.ModalSubmitInteractionCreate, s *session.Session) {
	// Parse user ID and get reason from modal
	userIDStr := event.Data.Text(constants.UserIDInputCustomID)
	reason := event.Data.Text(constants.ReasonInputCustomID)

	userID, err := strconv.ParseUint(userIDStr, 10, 64)
	if err != nil {
		m.Show(event, s, "Invalid user ID format.")
		return
	}

	// Store user ID for status tracking
	session.QueueUser.Set(s, userID)

	// Check if user is already in queue
	status, _, _, err := m.layout.queueManager.GetQueueInfo(context.Background(), userID)
	if err == nil && status != "" {
		// Show status menu if already queued
		m.layout.userReviewLayout.ShowStatusMenu(event, s)
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
		m.layout.paginationManager.RespondWithError(event, "Failed to add user to queue")
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
		m.layout.paginationManager.RespondWithError(event, "Failed to update queue info")
		return
	}

	// Show status menu to track progress
	m.layout.userReviewLayout.ShowStatusMenu(event, s)

	// Log the activity
	m.layout.db.Models().Activities().Log(context.Background(), &types.ActivityLog{
		ActivityTarget: types.ActivityTarget{
			UserID: userID,
		},
		ReviewerID:        uint64(event.User().ID),
		ActivityType:      enum.ActivityTypeUserRechecked,
		ActivityTimestamp: time.Now(),
		Details:           map[string]interface{}{"reason": reason},
	})
}

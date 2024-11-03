package queue

import (
	"context"
	"strconv"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/rotector/rotector/internal/bot/constants"
	"github.com/rotector/rotector/internal/bot/handlers/queue/builders"
	"github.com/rotector/rotector/internal/bot/interfaces"
	"github.com/rotector/rotector/internal/bot/pagination"
	"github.com/rotector/rotector/internal/bot/session"
	"github.com/rotector/rotector/internal/bot/utils"
	"github.com/rotector/rotector/internal/common/queue"
	"go.uber.org/zap"
)

// Menu handles the display and interaction logic for queue management.
// It works with the queue builder to show queue statistics and process
// user additions to different priority queues.
type Menu struct {
	handler *Handler
	page    *pagination.Page
}

// NewMenu creates a Menu and sets up its page with message builders and
// interaction handlers. The page is configured to show queue statistics
// and handle queue operations.
func NewMenu(h *Handler) *Menu {
	m := Menu{handler: h}
	m.page = &pagination.Page{
		Name: "Queue Menu",
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			// Load current queue lengths for display
			highCount := s.GetInt(constants.SessionKeyQueueHighCount)
			normalCount := s.GetInt(constants.SessionKeyQueueNormalCount)
			lowCount := s.GetInt(constants.SessionKeyQueueLowCount)

			return builders.NewQueueBuilder(highCount, normalCount, lowCount).Build()
		},
		SelectHandlerFunc: m.handleSelectMenu,
		ButtonHandlerFunc: m.handleButton,
		ModalHandlerFunc:  m.handleModal,
	}
	return &m
}

// ShowQueueMenu prepares and displays the queue interface by loading
// current queue lengths into the session.
func (m *Menu) ShowQueueMenu(event interfaces.CommonEvent, s *session.Session, content string) {
	// Store current queue lengths in session for the message builder
	s.Set(constants.SessionKeyQueueHighCount, m.handler.queueManager.GetQueueLength(context.Background(), queue.HighPriority))
	s.Set(constants.SessionKeyQueueNormalCount, m.handler.queueManager.GetQueueLength(context.Background(), queue.NormalPriority))
	s.Set(constants.SessionKeyQueueLowCount, m.handler.queueManager.GetQueueLength(context.Background(), queue.LowPriority))

	m.handler.paginationManager.NavigateTo(event, s, m.page, content)
}

// handleSelectMenu processes select menu interactions by storing the selected
// priority and showing a modal for user ID input.
func (m *Menu) handleSelectMenu(event *events.ComponentInteractionCreate, s *session.Session, customID string, option string) {
	if customID != constants.ActionSelectMenuCustomID {
		return
	}

	// Store the selected priority for the modal handler
	s.Set(constants.SessionKeyQueuePriority, option)

	// Create modal for user ID and reason input
	modal := discord.NewModalCreateBuilder().
		SetCustomID(constants.AddToQueueModalCustomID).
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
		m.handler.logger.Error("Failed to show modal", zap.Error(err))
	}
}

// handleButton processes button interactions, mainly handling navigation
// back to the dashboard and queue refresh requests.
func (m *Menu) handleButton(event *events.ComponentInteractionCreate, s *session.Session, customID string) {
	switch customID {
	case constants.BackButtonCustomID:
		m.handler.dashboardHandler.ShowDashboard(event, s, "")
	case constants.RefreshButtonCustomID:
		m.ShowQueueMenu(event, s, "")
	}
}

// handleModal processes modal submissions by adding the user to the queue
// with the specified priority and reason.
func (m *Menu) handleModal(event *events.ModalSubmitInteractionCreate, s *session.Session) {
	if event.Data.CustomID != constants.AddToQueueModalCustomID {
		return
	}

	// Parse user ID and get reason from modal
	userIDStr := event.Data.Text(constants.UserIDInputCustomID)
	reason := event.Data.Text(constants.ReasonInputCustomID)

	userID, err := strconv.ParseUint(userIDStr, 10, 64)
	if err != nil {
		m.ShowQueueMenu(event, s, "Invalid user ID format.")
		return
	}

	// Store user ID for status tracking
	s.Set(constants.SessionKeyQueueUser, userID)

	// Check if user is already in queue
	status, _, _, err := m.handler.queueManager.GetQueueInfo(context.Background(), userID)
	if err == nil && status != "" {
		// Show status menu if already queued
		m.handler.reviewHandler.ShowStatusMenu(event, s)
		return
	}

	// Add to queue with selected priority
	priority := s.GetString(constants.SessionKeyQueuePriority)
	err = m.handler.queueManager.AddToQueue(context.Background(), &queue.Item{
		UserID:      userID,
		Priority:    utils.GetPriorityFromCustomID(priority),
		Reason:      reason,
		AddedBy:     uint64(event.User().ID),
		AddedAt:     time.Now(),
		Status:      queue.StatusPending,
		CheckExists: false,
	})
	if err != nil {
		m.handler.paginationManager.RespondWithError(event, "Failed to add user to queue")
		return
	}

	// Update queue info with position
	err = m.handler.queueManager.SetQueueInfo(
		context.Background(),
		userID,
		queue.StatusPending,
		queue.HighPriority,
		m.handler.queueManager.GetQueueLength(context.Background(), queue.HighPriority),
	)
	if err != nil {
		m.handler.paginationManager.RespondWithError(event, "Failed to update queue info")
		return
	}

	// Show status menu to track progress
	m.handler.reviewHandler.ShowStatusMenu(event, s)
}

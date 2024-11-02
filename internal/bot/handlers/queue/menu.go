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

// Menu handles the queue management menu.
type Menu struct {
	handler *Handler
	page    *pagination.Page
}

// NewMenu creates a new Menu instance.
func NewMenu(h *Handler) *Menu {
	m := Menu{handler: h}
	m.page = &pagination.Page{
		Name: "Queue Menu",
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
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

// ShowQueueMenu displays the queue management menu.
func (m *Menu) ShowQueueMenu(event interfaces.CommonEvent, s *session.Session, content string) {
	s.Set(constants.SessionKeyQueueHighCount, m.handler.queueManager.GetQueueLength(context.Background(), queue.HighPriority))
	s.Set(constants.SessionKeyQueueNormalCount, m.handler.queueManager.GetQueueLength(context.Background(), queue.NormalPriority))
	s.Set(constants.SessionKeyQueueLowCount, m.handler.queueManager.GetQueueLength(context.Background(), queue.LowPriority))

	m.handler.paginationManager.NavigateTo(event, s, m.page, content)
}

// handleSelectMenu handles the select menu interactions.
func (m *Menu) handleSelectMenu(event *events.ComponentInteractionCreate, s *session.Session, customID string, option string) {
	if customID != constants.ActionSelectMenuCustomID {
		return
	}

	// Store the selected priority for the modal
	s.Set(constants.SessionKeyQueuePriority, option)

	// Show the modal for adding to queue
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

// handleButton handles button interactions.
func (m *Menu) handleButton(event *events.ComponentInteractionCreate, s *session.Session, customID string) {
	switch customID {
	case constants.BackButtonCustomID:
		m.handler.dashboardHandler.ShowDashboard(event, s, "")
	case constants.RefreshButtonCustomID:
		m.ShowQueueMenu(event, s, "")
	}
}

// handleModal handles modal submit interactions.
func (m *Menu) handleModal(event *events.ModalSubmitInteractionCreate, s *session.Session) {
	if event.Data.CustomID != constants.AddToQueueModalCustomID {
		return
	}

	// Get the user ID and reason from the modal
	userIDStr := event.Data.Text(constants.UserIDInputCustomID)
	reason := event.Data.Text(constants.ReasonInputCustomID)

	// Parse the user ID
	userID, err := strconv.ParseUint(userIDStr, 10, 64)
	if err != nil {
		m.ShowQueueMenu(event, s, "Invalid user ID format.")
		return
	}

	// Store the user ID in the session
	s.Set(constants.SessionKeyQueueUser, userID)

	// Check if user is already in queue
	status, _, _, err := m.handler.queueManager.GetQueueInfo(context.Background(), userID)
	if err == nil && status != "" {
		// User is already in queue, show status menu
		m.handler.reviewHandler.ShowStatusMenu(event, s)
		return
	}

	// Add to queue
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

	// Update queue info
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

	// Show status menu
	m.handler.reviewHandler.ShowStatusMenu(event, s)
}

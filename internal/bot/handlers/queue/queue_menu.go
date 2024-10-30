package queue

import (
	"strconv"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/rotector/rotector/internal/bot/constants"
	"github.com/rotector/rotector/internal/bot/handlers/queue/builders"
	"github.com/rotector/rotector/internal/bot/interfaces"
	"github.com/rotector/rotector/internal/bot/pagination"
	"github.com/rotector/rotector/internal/bot/session"
	"github.com/rotector/rotector/internal/common/queue"
	"go.uber.org/zap"
)

// QueueMenu handles the queue management menu.
type QueueMenu struct {
	handler *Handler
	page    *pagination.Page
}

// NewQueueMenu creates a new QueueMenu instance.
func NewQueueMenu(h *Handler) *QueueMenu {
	m := QueueMenu{handler: h}
	m.page = &pagination.Page{
		Name: "Queue Menu",
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			highCount := h.queueManager.GetQueueLength(queue.HighPriority)
			normalCount := h.queueManager.GetQueueLength(queue.NormalPriority)
			lowCount := h.queueManager.GetQueueLength(queue.LowPriority)
			return builders.NewQueueBuilder(highCount, normalCount, lowCount).Build()
		},
		SelectHandlerFunc: m.handleSelectMenu,
		ButtonHandlerFunc: m.handleButton,
		ModalHandlerFunc:  m.handleModal,
	}
	return &m
}

// ShowQueueMenu displays the queue management menu.
func (m *QueueMenu) ShowQueueMenu(event interfaces.CommonEvent, s *session.Session) {
	m.handler.paginationManager.NavigateTo(event, s, m.page, "")
}

// handleSelectMenu handles the select menu interactions.
func (m *QueueMenu) handleSelectMenu(event *events.ComponentInteractionCreate, s *session.Session, customID string, option string) {
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
func (m *QueueMenu) handleButton(event *events.ComponentInteractionCreate, s *session.Session, customID string) {
	if customID == constants.BackButtonCustomID {
		m.handler.dashboardHandler.ShowDashboard(event)
	}
}

// handleModal handles modal submit interactions.
func (m *QueueMenu) handleModal(event *events.ModalSubmitInteractionCreate, s *session.Session) {
	if event.Data.CustomID != constants.AddToQueueModalCustomID {
		return
	}

	// Get the user ID and reason from the modal
	userIDStr := event.Data.Text(constants.UserIDInputCustomID)
	reason := event.Data.Text(constants.ReasonInputCustomID)

	// Parse the user ID
	userID, err := strconv.ParseUint(userIDStr, 10, 64)
	if err != nil {
		m.handler.paginationManager.RespondWithError(event, "Invalid user ID format")
		return
	}

	// Get the selected priority
	priority := s.GetString(constants.SessionKeyQueuePriority)

	// Add to queue
	err = m.handler.queueManager.AddToQueue(&queue.Item{
		UserID:   userID,
		Priority: queue.GetPriorityFromCustomID(priority),
		Reason:   reason,
		AddedBy:  uint64(event.User().ID),
		AddedAt:  time.Now(),
		Status:   queue.StatusPending,
	})
	if err != nil {
		m.handler.paginationManager.RespondWithError(event, "Failed to add user to queue")
		return
	}

	// Show updated queue menu
	m.ShowQueueMenu(event, s)
}

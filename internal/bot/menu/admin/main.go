package admin

import (
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	builder "github.com/robalyx/rotector/internal/bot/builder/admin"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/pagination"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/interfaces"
	"go.uber.org/zap"
)

// MainMenu handles the admin operations and their interactions.
type MainMenu struct {
	layout *Layout
	page   *pagination.Page
}

// NewMainMenu creates a MainMenu and sets up its page with message builders and
// interaction handlers.
func NewMainMenu(layout *Layout) *MainMenu {
	m := &MainMenu{layout: layout}
	m.page = &pagination.Page{
		Name: "Admin Menu",
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return builder.NewMainBuilder(s).Build()
		},
		SelectHandlerFunc: m.handleSelectMenu,
		ButtonHandlerFunc: m.handleButton,
		ModalHandlerFunc:  m.handleModal,
	}
	return m
}

// Show prepares and displays the admin interface.
func (m *MainMenu) Show(event interfaces.CommonEvent, s *session.Session, content string) {
	m.layout.paginationManager.NavigateTo(event, s, m.page, content)
}

// handleSelectMenu processes select menu interactions.
func (m *MainMenu) handleSelectMenu(event *events.ComponentInteractionCreate, s *session.Session, _ string, option string) {
	switch option {
	case constants.BotSettingsButtonCustomID:
		m.layout.settingLayout.ShowBot(event, s)
	case constants.DeleteUserButtonCustomID:
		m.handleDeleteUserModal(event)
	case constants.DeleteGroupButtonCustomID:
		m.handleDeleteGroupModal(event)
	}
}

// handleDeleteUserModal opens a modal for entering a user ID to delete.
func (m *MainMenu) handleDeleteUserModal(event *events.ComponentInteractionCreate) {
	modal := discord.NewModalCreateBuilder().
		SetCustomID(constants.DeleteUserModalCustomID).
		SetTitle("Delete User").
		AddActionRow(
			discord.NewTextInput(constants.DeleteUserInputCustomID, discord.TextInputStyleShort, "User ID").
				WithRequired(true).
				WithPlaceholder("Enter the user ID to delete..."),
		).
		AddActionRow(
			discord.NewTextInput(constants.DeleteReasonInputCustomID, discord.TextInputStyleParagraph, "Reason").
				WithRequired(true).
				WithPlaceholder("Enter the reason for deletion...").
				WithMaxLength(512),
		).
		Build()

	if err := event.Modal(modal); err != nil {
		m.layout.logger.Error("Failed to create delete user modal", zap.Error(err))
		m.layout.paginationManager.RespondWithError(event, "Failed to open the delete user modal. Please try again.")
	}
}

// handleDeleteGroupModal opens a modal for entering a group ID to delete.
func (m *MainMenu) handleDeleteGroupModal(event *events.ComponentInteractionCreate) {
	modal := discord.NewModalCreateBuilder().
		SetCustomID(constants.DeleteGroupModalCustomID).
		SetTitle("Delete Group").
		AddActionRow(
			discord.NewTextInput(constants.DeleteGroupInputCustomID, discord.TextInputStyleShort, "Group ID").
				WithRequired(true).
				WithPlaceholder("Enter the group ID to delete..."),
		).
		AddActionRow(
			discord.NewTextInput(constants.DeleteReasonInputCustomID, discord.TextInputStyleParagraph, "Reason").
				WithRequired(true).
				WithPlaceholder("Enter the reason for deletion...").
				WithMaxLength(512),
		).
		Build()

	if err := event.Modal(modal); err != nil {
		m.layout.logger.Error("Failed to create delete group modal", zap.Error(err))
		m.layout.paginationManager.RespondWithError(event, "Failed to open the delete group modal. Please try again.")
	}
}

// handleButton processes button interactions.
func (m *MainMenu) handleButton(event *events.ComponentInteractionCreate, s *session.Session, customID string) {
	switch customID {
	case constants.BackButtonCustomID:
		m.layout.paginationManager.NavigateBack(event, s, "")
	}
}

// handleModal processes modal submissions.
func (m *MainMenu) handleModal(event *events.ModalSubmitInteractionCreate, s *session.Session) {
	switch event.Data.CustomID {
	case constants.DeleteUserModalCustomID:
		m.handleDeleteUserModalSubmit(event, s)
	case constants.DeleteGroupModalCustomID:
		m.handleDeleteGroupModalSubmit(event, s)
	}
}

// handleDeleteUserModalSubmit processes the user ID input and shows confirmation menu.
func (m *MainMenu) handleDeleteUserModalSubmit(event *events.ModalSubmitInteractionCreate, s *session.Session) {
	userID := event.Data.Text(constants.DeleteUserInputCustomID)
	reason := event.Data.Text(constants.DeleteReasonInputCustomID)
	s.Set(constants.SessionKeyDeleteID, userID)
	s.Set(constants.SessionKeyDeleteReason, reason)
	m.layout.confirmMenu.Show(event, s, constants.DeleteUserAction, "")
}

// handleDeleteGroupModalSubmit processes the group ID input and shows confirmation menu.
func (m *MainMenu) handleDeleteGroupModalSubmit(event *events.ModalSubmitInteractionCreate, s *session.Session) {
	groupID := event.Data.Text(constants.DeleteGroupInputCustomID)
	reason := event.Data.Text(constants.DeleteReasonInputCustomID)
	s.Set(constants.SessionKeyDeleteID, groupID)
	s.Set(constants.SessionKeyDeleteReason, reason)
	m.layout.confirmMenu.Show(event, s, constants.DeleteGroupAction, "")
}

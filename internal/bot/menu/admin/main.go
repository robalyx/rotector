package admin

import (
	"errors"
	"fmt"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	builder "github.com/robalyx/rotector/internal/bot/builder/admin"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/pagination"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/interfaces"
	"github.com/robalyx/rotector/internal/bot/utils"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
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
		Name: constants.AdminPageName,
		Message: func(s *session.Session) *discord.MessageUpdateBuilder {
			return builder.NewBuilder(s).Build()
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
	case constants.BanUserButtonCustomID:
		m.handleBanUserModal(event)
	case constants.UnbanUserButtonCustomID:
		m.handleUnbanUserModal(event)
	case constants.DeleteUserButtonCustomID:
		m.handleDeleteUserModal(event)
	case constants.DeleteGroupButtonCustomID:
		m.handleDeleteGroupModal(event)
	}
}

// handleBanUserModal opens a modal for entering a user ID to ban.
func (m *MainMenu) handleBanUserModal(event *events.ComponentInteractionCreate) {
	modal := discord.NewModalCreateBuilder().
		SetCustomID(constants.BanUserModalCustomID).
		SetTitle("Ban User").
		AddActionRow(
			discord.NewTextInput(constants.BanUserInputCustomID, discord.TextInputStyleShort, "User ID").
				WithRequired(true).
				WithPlaceholder("Enter the user ID to ban..."),
		).
		AddActionRow(
			discord.NewTextInput(constants.BanTypeInputCustomID, discord.TextInputStyleShort, "Ban Type").
				WithRequired(true).
				WithPlaceholder("abuse, inappropriate, or other").
				WithMaxLength(20),
		).
		AddActionRow(
			discord.NewTextInput(constants.BanDurationInputCustomID, discord.TextInputStyleShort, "Duration").
				WithRequired(false).
				WithPlaceholder("e.g. 2h45m, 2h, 5m, 1s or leave empty for permanent").
				WithMaxLength(10),
		).
		AddActionRow(
			discord.NewTextInput(constants.AdminReasonInputCustomID, discord.TextInputStyleParagraph, "Ban Notes").
				WithRequired(true).
				WithPlaceholder("Enter notes about this ban...").
				WithMaxLength(512),
		).
		Build()

	if err := event.Modal(modal); err != nil {
		m.layout.logger.Error("Failed to create ban user modal", zap.Error(err))
		m.layout.paginationManager.RespondWithError(event, "Failed to open the ban user modal. Please try again.")
	}
}

// handleUnbanUserModal opens a modal for entering a user ID to unban.
func (m *MainMenu) handleUnbanUserModal(event *events.ComponentInteractionCreate) {
	modal := discord.NewModalCreateBuilder().
		SetCustomID(constants.UnbanUserModalCustomID).
		SetTitle("Unban User").
		AddActionRow(
			discord.NewTextInput(constants.UnbanUserInputCustomID, discord.TextInputStyleShort, "User ID").
				WithRequired(true).
				WithPlaceholder("Enter the user ID to unban..."),
		).
		AddActionRow(
			discord.NewTextInput(constants.AdminReasonInputCustomID, discord.TextInputStyleParagraph, "Unban Notes").
				WithRequired(true).
				WithPlaceholder("Enter notes about this unban...").
				WithMaxLength(512),
		).
		Build()

	if err := event.Modal(modal); err != nil {
		m.layout.logger.Error("Failed to create unban user modal", zap.Error(err))
		m.layout.paginationManager.RespondWithError(event, "Failed to open the unban user modal. Please try again.")
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
			discord.NewTextInput(constants.AdminReasonInputCustomID, discord.TextInputStyleParagraph, "Reason").
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
			discord.NewTextInput(constants.AdminReasonInputCustomID, discord.TextInputStyleParagraph, "Reason").
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
	case constants.BanUserModalCustomID:
		m.handleBanUserModalSubmit(event, s)
	case constants.UnbanUserModalCustomID:
		m.handleUnbanUserModalSubmit(event, s)
	case constants.DeleteUserModalCustomID:
		m.handleDeleteUserModalSubmit(event, s)
	case constants.DeleteGroupModalCustomID:
		m.handleDeleteGroupModalSubmit(event, s)
	}
}

// handleBanUserModalSubmit processes the user ID input and shows confirmation menu.
func (m *MainMenu) handleBanUserModalSubmit(event *events.ModalSubmitInteractionCreate, s *session.Session) {
	userID := event.Data.Text(constants.BanUserInputCustomID)
	banType := event.Data.Text(constants.BanTypeInputCustomID)
	notes := event.Data.Text(constants.AdminReasonInputCustomID)
	duration := event.Data.Text(constants.BanDurationInputCustomID)

	// Parse ban type
	banReason, err := enum.BanReasonString(banType)
	if err != nil {
		banReason = enum.BanReasonOther
	}

	// Parse ban duration
	expiresAt, err := utils.ParseBanDuration(duration)
	if err != nil && !errors.Is(err, utils.ErrPermanentBan) {
		m.layout.logger.Debug("Failed to parse ban duration", zap.Error(err))
		m.layout.paginationManager.Refresh(event, s, fmt.Sprintf("Failed to parse ban duration: %s", err))
		return
	}

	session.AdminActionID.Set(s, userID)
	session.AdminReason.Set(s, notes)
	session.AdminBanReason.Set(s, banReason)
	session.AdminBanExpiry.Set(s, expiresAt) // Will be nil for permanent bans
	m.layout.confirmMenu.Show(event, s, constants.BanUserAction, "")
}

// handleUnbanUserModalSubmit processes the user ID input and shows confirmation menu.
func (m *MainMenu) handleUnbanUserModalSubmit(event *events.ModalSubmitInteractionCreate, s *session.Session) {
	userID := event.Data.Text(constants.UnbanUserInputCustomID)
	notes := event.Data.Text(constants.AdminReasonInputCustomID)

	session.AdminActionID.Set(s, userID)
	session.AdminReason.Set(s, notes)
	m.layout.confirmMenu.Show(event, s, constants.UnbanUserAction, "")
}

// handleDeleteUserModalSubmit processes the user ID input and shows confirmation menu.
func (m *MainMenu) handleDeleteUserModalSubmit(event *events.ModalSubmitInteractionCreate, s *session.Session) {
	userID := event.Data.Text(constants.DeleteUserInputCustomID)
	reason := event.Data.Text(constants.AdminReasonInputCustomID)

	session.AdminActionID.Set(s, userID)
	session.AdminReason.Set(s, reason)
	m.layout.confirmMenu.Show(event, s, constants.DeleteUserAction, "")
}

// handleDeleteGroupModalSubmit processes the group ID input and shows confirmation menu.
func (m *MainMenu) handleDeleteGroupModalSubmit(event *events.ModalSubmitInteractionCreate, s *session.Session) {
	groupID := event.Data.Text(constants.DeleteGroupInputCustomID)
	reason := event.Data.Text(constants.AdminReasonInputCustomID)

	session.AdminActionID.Set(s, groupID)
	session.AdminReason.Set(s, reason)
	m.layout.confirmMenu.Show(event, s, constants.DeleteGroupAction, "")
}

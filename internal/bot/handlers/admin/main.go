package admin

import (
	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/interaction"
	"github.com/robalyx/rotector/internal/bot/core/session"
	builder "github.com/robalyx/rotector/internal/bot/views/admin"
)

// MainMenu handles the admin operations and their interactions.
type MainMenu struct {
	layout *Layout
	page   *interaction.Page
}

// NewMainMenu creates a MainMenu and sets up its page with message builders and
// interaction handlers.
func NewMainMenu(layout *Layout) *MainMenu {
	m := &MainMenu{layout: layout}
	m.page = &interaction.Page{
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

// handleSelectMenu processes select menu interactions.
func (m *MainMenu) handleSelectMenu(ctx *interaction.Context, _ *session.Session, _, option string) {
	switch option {
	case constants.BotSettingsButtonCustomID:
		ctx.Show(constants.BotSettingsPageName, "")
	case constants.DeleteUserButtonCustomID:
		m.handleDeleteUserModal(ctx)
	case constants.DeleteGroupButtonCustomID:
		m.handleDeleteGroupModal(ctx)
	}
}

// handleDeleteUserModal opens a modal for entering a user ID to delete.
func (m *MainMenu) handleDeleteUserModal(ctx *interaction.Context) {
	modal := discord.NewModalCreateBuilder().
		SetCustomID(constants.DeleteUserModalCustomID).
		SetTitle("Delete User").
		AddLabel(
			"User ID",
			discord.NewTextInput(constants.DeleteUserInputCustomID, discord.TextInputStyleShort).
				WithRequired(true).
				WithPlaceholder("Enter the user ID to delete..."),
		).
		AddLabel(
			"Reason",
			discord.NewTextInput(constants.AdminReasonInputCustomID, discord.TextInputStyleParagraph).
				WithRequired(true).
				WithPlaceholder("Enter the reason for deletion...").
				WithMaxLength(512),
		)

	ctx.Modal(modal)
}

// handleDeleteGroupModal opens a modal for entering a group ID to delete.
func (m *MainMenu) handleDeleteGroupModal(ctx *interaction.Context) {
	modal := discord.NewModalCreateBuilder().
		SetCustomID(constants.DeleteGroupModalCustomID).
		SetTitle("Delete Group").
		AddLabel(
			"Group ID",
			discord.NewTextInput(constants.DeleteGroupInputCustomID, discord.TextInputStyleShort).
				WithRequired(true).
				WithPlaceholder("Enter the group ID to delete..."),
		).
		AddLabel(
			"Reason",
			discord.NewTextInput(constants.AdminReasonInputCustomID, discord.TextInputStyleParagraph).
				WithRequired(true).
				WithPlaceholder("Enter the reason for deletion...").
				WithMaxLength(512),
		)

	ctx.Modal(modal)
}

// handleButton processes button interactions.
func (m *MainMenu) handleButton(ctx *interaction.Context, _ *session.Session, customID string) {
	switch customID {
	case constants.BackButtonCustomID:
		ctx.NavigateBack("")
	}
}

// handleModal processes modal submissions.
func (m *MainMenu) handleModal(ctx *interaction.Context, s *session.Session) {
	switch ctx.Event().CustomID() {
	case constants.DeleteUserModalCustomID:
		m.handleDeleteUserModalSubmit(ctx, s)
	case constants.DeleteGroupModalCustomID:
		m.handleDeleteGroupModalSubmit(ctx, s)
	}
}

// handleDeleteUserModalSubmit processes the user ID input and shows confirmation menu.
func (m *MainMenu) handleDeleteUserModalSubmit(ctx *interaction.Context, s *session.Session) {
	// Get data from modal
	data := ctx.Event().ModalData()
	userID := data.Text(constants.DeleteUserInputCustomID)
	reason := data.Text(constants.AdminReasonInputCustomID)

	session.AdminAction.Set(s, constants.DeleteUserAction)
	session.AdminActionID.Set(s, userID)
	session.AdminReason.Set(s, reason)
	ctx.Show(constants.AdminActionConfirmPageName, "")
}

// handleDeleteGroupModalSubmit processes the group ID input and shows confirmation menu.
func (m *MainMenu) handleDeleteGroupModalSubmit(ctx *interaction.Context, s *session.Session) {
	// Get data from modal
	data := ctx.Event().ModalData()
	groupID := data.Text(constants.DeleteGroupInputCustomID)
	reason := data.Text(constants.AdminReasonInputCustomID)

	session.AdminAction.Set(s, constants.DeleteGroupAction)
	session.AdminActionID.Set(s, groupID)
	session.AdminReason.Set(s, reason)
	ctx.Show(constants.AdminActionConfirmPageName, "")
}

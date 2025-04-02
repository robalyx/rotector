package admin

import (
	"errors"
	"fmt"

	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/interaction"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/utils"
	builder "github.com/robalyx/rotector/internal/bot/views/admin"
	"github.com/robalyx/rotector/internal/database/types/enum"
	"go.uber.org/zap"
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
	case constants.BanUserButtonCustomID:
		m.handleBanUserModal(ctx)
	case constants.UnbanUserButtonCustomID:
		m.handleUnbanUserModal(ctx)
	case constants.DeleteUserButtonCustomID:
		m.handleDeleteUserModal(ctx)
	case constants.DeleteGroupButtonCustomID:
		m.handleDeleteGroupModal(ctx)
	}
}

// handleBanUserModal opens a modal for entering a user ID to ban.
func (m *MainMenu) handleBanUserModal(ctx *interaction.Context) {
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
		)

	ctx.Modal(modal)
}

// handleUnbanUserModal opens a modal for entering a user ID to unban.
func (m *MainMenu) handleUnbanUserModal(ctx *interaction.Context) {
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
		)

	ctx.Modal(modal)
}

// handleDeleteUserModal opens a modal for entering a user ID to delete.
func (m *MainMenu) handleDeleteUserModal(ctx *interaction.Context) {
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
		)

	ctx.Modal(modal)
}

// handleDeleteGroupModal opens a modal for entering a group ID to delete.
func (m *MainMenu) handleDeleteGroupModal(ctx *interaction.Context) {
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
	case constants.BanUserModalCustomID:
		m.handleBanUserModalSubmit(ctx, s)
	case constants.UnbanUserModalCustomID:
		m.handleUnbanUserModalSubmit(ctx, s)
	case constants.DeleteUserModalCustomID:
		m.handleDeleteUserModalSubmit(ctx, s)
	case constants.DeleteGroupModalCustomID:
		m.handleDeleteGroupModalSubmit(ctx, s)
	}
}

// handleBanUserModalSubmit processes the user ID input and shows confirmation menu.
func (m *MainMenu) handleBanUserModalSubmit(ctx *interaction.Context, s *session.Session) {
	// Get data from modal
	data := ctx.Event().ModalData()
	userID := data.Text(constants.BanUserInputCustomID)
	banType := data.Text(constants.BanTypeInputCustomID)
	notes := data.Text(constants.AdminReasonInputCustomID)
	duration := data.Text(constants.BanDurationInputCustomID)

	// Parse ban type
	banReason, err := enum.BanReasonString(banType)
	if err != nil {
		banReason = enum.BanReasonOther
	}

	// Parse ban duration
	expiresAt, err := utils.ParseBanDuration(duration)
	if err != nil && !errors.Is(err, utils.ErrPermanentBan) {
		m.layout.logger.Debug("Failed to parse ban duration", zap.Error(err))
		ctx.Cancel(fmt.Sprintf("Ban duration is invalid: %s", err))
		return
	}

	session.AdminAction.Set(s, constants.BanUserAction)
	session.AdminActionID.Set(s, userID)
	session.AdminReason.Set(s, notes)
	session.AdminBanReason.Set(s, banReason)
	session.AdminBanExpiry.Set(s, expiresAt) // Will be nil for permanent bans
	ctx.Show(constants.AdminActionConfirmPageName, "")
}

// handleUnbanUserModalSubmit processes the user ID input and shows confirmation menu.
func (m *MainMenu) handleUnbanUserModalSubmit(ctx *interaction.Context, s *session.Session) {
	// Get data from modal
	data := ctx.Event().ModalData()
	userID := data.Text(constants.UnbanUserInputCustomID)
	notes := data.Text(constants.AdminReasonInputCustomID)

	session.AdminAction.Set(s, constants.UnbanUserAction)
	session.AdminActionID.Set(s, userID)
	session.AdminReason.Set(s, notes)
	ctx.Show(constants.AdminActionConfirmPageName, "")
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

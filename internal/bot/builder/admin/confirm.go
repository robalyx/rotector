package admin

import (
	"github.com/disgoorg/disgo/discord"
	"github.com/rotector/rotector/internal/bot/constants"
	"github.com/rotector/rotector/internal/bot/core/session"
)

// ConfirmBuilder creates the visual layout for the confirmation menu.
type ConfirmBuilder struct {
	action string
	id     string
}

// NewConfirmBuilder creates a new confirmation menu builder.
func NewConfirmBuilder(s *session.Session) *ConfirmBuilder {
	return &ConfirmBuilder{
		action: s.GetString(constants.SessionKeyDeleteAction),
		id:     s.GetString(constants.SessionKeyDeleteID),
	}
}

// Build creates a Discord message with confirmation options.
func (b *ConfirmBuilder) Build() *discord.MessageUpdateBuilder {
	var title, description string
	if b.action == constants.DeleteUserAction {
		title = "Confirm User Deletion"
		description = "Are you sure you want to delete user `" + b.id + "` from the database?"
	} else {
		title = "Confirm Group Deletion"
		description = "Are you sure you want to delete group `" + b.id + "` from the database?"
	}

	embed := discord.NewEmbedBuilder().
		SetTitle(title).
		SetDescription("⚠️ **Warning**: " + description).
		SetColor(constants.DefaultEmbedColor)

	return discord.NewMessageUpdateBuilder().
		SetEmbeds(embed.Build()).
		AddActionRow(
			discord.NewSecondaryButton("◀️", constants.BackButtonCustomID),
			discord.NewDangerButton("Confirm", constants.DeleteConfirmButtonCustomID),
		)
}

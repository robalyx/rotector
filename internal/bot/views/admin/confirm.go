package admin

import (
	"fmt"
	"strings"

	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/session"
)

// ConfirmBuilder creates the visual layout for the confirmation menu.
type ConfirmBuilder struct {
	action string
	id     string
	reason string
}

// NewConfirmBuilder creates a new confirmation menu builder.
func NewConfirmBuilder(s *session.Session) *ConfirmBuilder {
	return &ConfirmBuilder{
		action: session.AdminAction.Get(s),
		id:     session.AdminActionID.Get(s),
		reason: session.AdminReason.Get(s),
	}
}

// Build creates a Discord message with confirmation options.
func (b *ConfirmBuilder) Build() *discord.MessageUpdateBuilder {
	var title, description string

	// Build title and description based on action
	switch b.action {
	case constants.DeleteUserAction:
		title = "Confirm Roblox User Deletion"
		description = fmt.Sprintf("Are you sure you want to delete roblox user `%s` from the database?", b.id)

	case constants.DeleteGroupAction:
		title = "Confirm Roblox Group Deletion"
		description = fmt.Sprintf("Are you sure you want to delete roblox group `%s` from the database?", b.id)
	}

	// Build content based on action type
	var content strings.Builder
	content.WriteString(fmt.Sprintf("## %s\n⚠️ **Warning**: %s\n", title, description))

	// Add fields based on action type
	switch b.action {
	case constants.DeleteUserAction, constants.DeleteGroupAction:
		content.WriteString("\n### Reason\n" + b.reason)
	}

	// Create main container
	mainContainer := discord.NewContainer(
		discord.NewTextDisplay(content.String()),
		discord.NewLargeSeparator(),
		discord.NewActionRow(
			discord.NewDangerButton("Confirm", constants.ActionButtonCustomID),
		),
	).WithAccentColor(constants.DefaultContainerColor)

	return discord.NewMessageUpdateBuilder().
		AddComponents(
			mainContainer,
			discord.NewActionRow(
				discord.NewSecondaryButton("◀️ Back", constants.BackButtonCustomID),
			),
		)
}

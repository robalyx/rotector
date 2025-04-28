package admin

import (
	"fmt"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/database/types/enum"
)

// ConfirmBuilder creates the visual layout for the confirmation menu.
type ConfirmBuilder struct {
	action    string
	id        string
	reason    string
	banReason enum.BanReason
	expiresAt *time.Time
}

// NewConfirmBuilder creates a new confirmation menu builder.
func NewConfirmBuilder(s *session.Session) *ConfirmBuilder {
	return &ConfirmBuilder{
		action:    session.AdminAction.Get(s),
		id:        session.AdminActionID.Get(s),
		reason:    session.AdminReason.Get(s),
		banReason: session.AdminBanReason.Get(s),
		expiresAt: session.AdminBanExpiry.Get(s),
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

	case constants.BanUserAction:
		title = "Confirm Discord User Ban"
		description = fmt.Sprintf("Are you sure you want to ban Discord user `%s`?", b.id)

	case constants.UnbanUserAction:
		title = "Confirm Discord User Unban"
		description = fmt.Sprintf("Are you sure you want to unban Discord user `%s`?", b.id)
	}

	// Build content based on action type
	var content strings.Builder
	content.WriteString(fmt.Sprintf("## %s\n⚠️ **Warning**: %s\n", title, description))

	// Add fields based on action type
	switch b.action {
	case constants.DeleteUserAction, constants.DeleteGroupAction:
		content.WriteString("\n### Reason\n" + b.reason)

	case constants.BanUserAction:
		content.WriteString("\n### Ban Details\n")
		content.WriteString(fmt.Sprintf("-# Ban Reason: %s\n", b.banReason.String()))

		if b.expiresAt != nil {
			content.WriteString(fmt.Sprintf("-# Expires: <t:%d:f>\n", b.expiresAt.Unix()))
		} else {
			content.WriteString("-# Duration: Permanent\n")
		}

		content.WriteString("\n### Notes\n" + b.reason)

	case constants.UnbanUserAction:
		content.WriteString("\n### Notes\n" + b.reason)
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

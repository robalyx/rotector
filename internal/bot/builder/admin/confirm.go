package admin

import (
	"fmt"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/session"
)

// ConfirmBuilder creates the visual layout for the confirmation menu.
type ConfirmBuilder struct {
	action  string
	id      string
	reason  string
	session *session.Session
}

// NewConfirmBuilder creates a new confirmation menu builder.
func NewConfirmBuilder(s *session.Session) *ConfirmBuilder {
	return &ConfirmBuilder{
		action:  s.GetString(constants.SessionKeyAdminAction),
		id:      s.GetString(constants.SessionKeyAdminActionID),
		reason:  s.GetString(constants.SessionKeyAdminReason),
		session: s,
	}
}

// Build creates a Discord message with confirmation options.
func (b *ConfirmBuilder) Build() *discord.MessageUpdateBuilder {
	var title, description string
	embed := discord.NewEmbedBuilder()

	switch b.action {
	case constants.DeleteUserAction:
		title = "Confirm Roblox User Deletion"
		description = "Are you sure you want to delete roblox user `" + b.id + "` from the database?"
		embed.AddField("Reason", b.reason, false)

	case constants.DeleteGroupAction:
		title = "Confirm Roblox Group Deletion"
		description = "Are you sure you want to delete roblox group `" + b.id + "` from the database?"
		embed.AddField("Reason", b.reason, false)

	case constants.BanUserAction:
		title = "Confirm Discord User Ban"
		description = "Are you sure you want to ban Discord user `" + b.id + "`?"

		// Add ban type field
		banType := b.session.GetString(constants.SessionKeyBanType)
		embed.AddField("Ban Type", banType, true)

		// Add duration/expiry field
		var expiresAt *time.Time
		b.session.GetInterface(constants.SessionKeyBanExpiry, &expiresAt)
		if expiresAt != nil {
			embed.AddField("Expires", fmt.Sprintf("<t:%d:f>", expiresAt.Unix()), true)
		} else {
			embed.AddField("Duration", "Permanent", true)
		}

		// Add notes field
		embed.AddField("Notes", b.reason, false)

	case constants.UnbanUserAction:
		title = "Confirm Discord User Unban"
		description = "Are you sure you want to unban Discord user `" + b.id + "`?"
		embed.AddField("Notes", b.reason, false)
	}

	embed.SetTitle(title).
		SetDescription("⚠️ **Warning**: " + description).
		SetColor(constants.DefaultEmbedColor)

	return discord.NewMessageUpdateBuilder().
		SetEmbeds(embed.Build()).
		AddActionRow(
			discord.NewSecondaryButton("◀️", constants.BackButtonCustomID),
			discord.NewDangerButton("Confirm", constants.ActionButtonCustomID),
		)
}

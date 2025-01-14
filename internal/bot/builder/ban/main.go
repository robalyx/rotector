package ban

import (
	"fmt"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
)

// Builder creates the visual layout for the ban information menu.
type Builder struct {
	ban *types.DiscordBan
}

// NewBuilder creates a new ban menu builder.
func NewBuilder(s *session.Session) *Builder {
	var ban *types.DiscordBan
	s.GetInterface(constants.SessionKeyBanInfo, &ban)

	return &Builder{
		ban: ban,
	}
}

// Build creates a Discord message showing ban information.
func (b *Builder) Build() *discord.MessageUpdateBuilder {
	// Create embed
	embed := discord.NewEmbedBuilder().
		SetTitle("Access Denied").
		SetDescription("You are currently banned from using this bot.").
		AddField("Ban Reason", b.ban.Reason.String(), true).
		AddField("Ban Source", b.ban.Source.String(), true)

	// Add notes if any
	if b.ban.Notes != "" {
		embed.AddField("Additional Notes", b.ban.Notes, false)
	}

	// Format duration if temporary ban
	if b.ban.IsPermanent() {
		embed.AddField("Duration", "Permanent", true)
	} else {
		remaining := time.Until(*b.ban.ExpiresAt)
		if remaining <= 0 {
			embed.AddField("Duration", "Expired", true)
		} else {
			embed.AddField("Duration", fmt.Sprintf("Expires <t:%d:R>", b.ban.ExpiresAt.Unix()), true)
		}
	}

	embed.AddField("Banned On", fmt.Sprintf("<t:%d:f>", b.ban.BannedAt.Unix()), true)

	// Add appeal instructions
	embed.AddField("Appeals",
		"Bans are non-appealable. If you believe this ban was caused by a system error, please contact a staff member.", false)

	embed.SetColor(constants.ErrorEmbedColor)

	return discord.NewMessageUpdateBuilder().
		SetEmbeds(embed.Build())
}

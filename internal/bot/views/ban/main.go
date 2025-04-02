package ban

import (
	"fmt"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/utils"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/database/types/enum"
)

// Builder creates the visual layout for the ban information menu.
type Builder struct {
	ban         *types.DiscordBan
	maintenance bool
}

// NewBuilder creates a new ban menu builder.
func NewBuilder(s *session.Session) *Builder {
	return &Builder{
		ban:         session.AdminBanInfo.Get(s),
		maintenance: session.BotAnnouncementType.Get(s) == enum.AnnouncementTypeMaintenance,
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
		embed.AddField("Additional Notes", utils.FormatString(b.ban.Notes), false)
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
	embed.AddField("Appeals", "Both Discord and Roblox bans can be appealed using the button below. "+
		"However, as a precautionary measure, access to other parts of the Discord bot will remain "+
		"restricted for users with a history of violations.\n\n"+
		"If you believe this restriction was caused by a system error, please contact a staff member.", false)

	embed.SetColor(constants.ErrorEmbedColor)

	// Create message with embed and appeals button
	builder := discord.NewMessageUpdateBuilder().
		SetEmbeds(embed.Build())

	// Only show appeals button if not in maintenance mode
	if !b.maintenance {
		builder.AddActionRow(
			discord.NewPrimaryButton("View Appeals", constants.AppealMenuButtonCustomID),
		)
	}

	return builder
}

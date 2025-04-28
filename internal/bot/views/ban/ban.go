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
	// Create main container
	var mainDisplays []discord.ContainerSubComponent

	// Add header
	mainDisplays = append(mainDisplays,
		discord.NewTextDisplay("## Access Denied\nYou are currently banned from using this bot."),
		discord.NewLargeSeparator(),
	)

	// Format duration text
	var durationText string
	if b.ban.IsPermanent() {
		durationText = "Permanent"
	} else {
		remaining := time.Until(*b.ban.ExpiresAt)
		if remaining <= 0 {
			durationText = "Expired"
		} else {
			durationText = fmt.Sprintf("Expires <t:%d:R>", b.ban.ExpiresAt.Unix())
		}
	}

	// Add ban details including duration
	mainDisplays = append(mainDisplays,
		discord.NewTextDisplay(fmt.Sprintf("### Ban Details\n- Ban Reason: %s\n- Ban Source: %s\n- Duration: %s\n- Banned On: <t:%d:f>",
			b.ban.Reason.String(),
			b.ban.Source.String(),
			durationText,
			b.ban.BannedAt.Unix())),
	)

	// Add notes if any
	if b.ban.Notes != "" {
		mainDisplays = append(mainDisplays,
			discord.NewLargeSeparator(),
			discord.NewTextDisplay("### Additional Notes\n"+utils.FormatString(b.ban.Notes)),
		)
	}

	// Add appeal instructions
	mainDisplays = append(mainDisplays,
		discord.NewLargeSeparator(),
		discord.NewTextDisplay("### Appeals\nBoth Discord and Roblox bans can be appealed using the button below. "+
			"However, as a precautionary measure, access to other parts of the Discord bot will remain "+
			"restricted for users with a history of violations.\n\n"+
			"If you believe this restriction was caused by a system error, please contact a staff member."),
	)

	// Add appeals button if not in maintenance mode
	if !b.maintenance {
		mainDisplays = append(mainDisplays,
			discord.NewLargeSeparator(),
			discord.NewActionRow(
				discord.NewPrimaryButton("View Appeals", constants.AppealMenuButtonCustomID),
			),
		)
	}

	mainContainer := discord.NewContainer(mainDisplays...).
		WithAccentColor(constants.ErrorContainerColor)

	return discord.NewMessageUpdateBuilder().
		AddComponents(mainContainer)
}

//nolint:lll
package guild

import (
	"strconv"

	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/session"
)

// MenuBuilder creates the visual layout for the guild owner tools menu.
type MenuBuilder struct {
	guildName          string
	uniqueGuilds       int
	uniqueUsers        int
	inappropriateUsers int
}

// NewMenuBuilder creates a new menu builder.
func NewMenuBuilder(s *session.Session) *MenuBuilder {
	return &MenuBuilder{
		guildName:          session.GuildStatsName.Get(s),
		uniqueGuilds:       session.GuildStatsUniqueGuilds.Get(s),
		uniqueUsers:        session.GuildStatsUniqueUsers.Get(s),
		inappropriateUsers: session.GuildStatsInappropriateUsers.Get(s),
	}
}

// Build creates a Discord message with the guild owner tools.
func (b *MenuBuilder) Build() *discord.MessageUpdateBuilder {
	// Create main and stats embeds
	mainEmbed := b.buildMainEmbed()
	statsEmbed := b.buildStatsEmbed()

	// Create action menu
	options := []discord.StringSelectMenuOption{
		discord.NewStringSelectMenuOption("Ban Users in Condo Servers", constants.StartGuildScanButtonCustomID).
			WithEmoji(discord.ComponentEmoji{Name: "🔍"}).
			WithDescription("Scan and remove users who are members of flagged servers"),
		discord.NewStringSelectMenuOption("Ban Users with Inappropriate Messages", constants.StartMessageScanButtonCustomID).
			WithEmoji(discord.ComponentEmoji{Name: "💬"}).
			WithDescription("Scan and remove users who sent inappropriate messages (Recommended)"),
		discord.NewStringSelectMenuOption("View Ban Logs", constants.ViewGuildBanLogsButtonCustomID).
			WithEmoji(discord.ComponentEmoji{Name: "📜"}).
			WithDescription("View history of ban operations"),
	}

	return discord.NewMessageUpdateBuilder().
		SetEmbeds(mainEmbed.Build(), statsEmbed.Build()).
		AddContainerComponents(
			discord.NewActionRow(
				discord.NewStringSelectMenu(constants.ActionSelectMenuCustomID, "Select an action", options...),
			),
			discord.NewActionRow(
				discord.NewSecondaryButton("◀️ Back", constants.BackButtonCustomID),
			),
		)
}

// buildMainEmbed creates the main embed describing guild owner tools.
func (b *MenuBuilder) buildMainEmbed() *discord.EmbedBuilder {
	embed := discord.NewEmbedBuilder().
		SetTitle("Guild Owner Tools").
		SetDescription("These tools help you maintain a safe server environment by managing Discord users in your server who may be participating in ERP (erotic roleplay) across multiple Discord servers.").
		AddField("Getting Started",
			"Select an option from the dropdown menu below to begin. The scan tool will check your server members against our database of flagged users, allowing you to identify and remove those participating in inappropriate content. The logs will show you a history of previous ban operations.",
			false).
		SetColor(constants.DefaultEmbedColor).
		SetFooter("Inspired by Ruben Sim's Ro-Cleaner bot", "")

	if b.guildName != "" {
		embed.AddField("Current Guild", b.guildName, false)
	}

	return embed
}

// buildStatsEmbed creates a statistics embed showing sync information.
func (b *MenuBuilder) buildStatsEmbed() *discord.EmbedBuilder {
	guildCountText := strconv.Itoa(b.uniqueGuilds)
	userCountText := strconv.Itoa(b.uniqueUsers)
	inappropriateUsersText := strconv.Itoa(b.inappropriateUsers)

	return discord.NewEmbedBuilder().
		SetTitle("Sync Statistics").
		SetDescription("Current tracking information from our database.").
		AddField("Tracked Discord Servers", guildCountText, true).
		AddField("Tracked Discord Users", userCountText, true).
		AddField("Inappropriate Discord Users", inappropriateUsersText, true).
		SetColor(constants.DefaultEmbedColor)
}

package guild

import (
	"fmt"
	"strconv"

	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
)

// LookupBuilder creates the visual layout for the Discord user lookup interface.
type LookupBuilder struct {
	userID         uint64
	username       string
	userGuilds     []*types.UserGuildInfo
	guildNames     map[uint64]string
	messageSummary *types.InappropriateUserSummary
	messageGuilds  map[uint64]struct{}
	totalGuilds    int
	hasNextPage    bool
	hasPrevPage    bool
	isDataRedacted bool
}

// NewLookupBuilder creates a new Discord user builder.
func NewLookupBuilder(s *session.Session) *LookupBuilder {
	return &LookupBuilder{
		userID:         session.DiscordUserLookupID.Get(s),
		username:       session.DiscordUserLookupName.Get(s),
		userGuilds:     session.DiscordUserGuilds.Get(s),
		guildNames:     session.DiscordUserGuildNames.Get(s),
		messageSummary: session.DiscordUserMessageSummary.Get(s),
		messageGuilds:  session.DiscordUserMessageGuilds.Get(s),
		totalGuilds:    session.DiscordUserTotalGuilds.Get(s),
		hasNextPage:    session.PaginationHasNextPage.Get(s),
		hasPrevPage:    session.PaginationHasPrevPage.Get(s),
		isDataRedacted: session.DiscordUserDataRedacted.Get(s),
	}
}

// Build creates a Discord message showing the user's flagged guild memberships.
func (b *LookupBuilder) Build() *discord.MessageUpdateBuilder {
	builder := discord.NewMessageUpdateBuilder()

	// Add data deletion notice if data is redacted
	if b.isDataRedacted {
		builder.AddEmbeds(b.buildDeletionEmbed().Build())
	}

	// Add main embeds
	builder.AddEmbeds(
		b.buildUserEmbed().Build(),
		b.buildGuildsEmbed().Build(),
	)

	// Add components
	builder.AddContainerComponents(b.buildComponents()...)

	return builder
}

// buildDeletionEmbed creates an embed notifying that the user has requested data deletion.
func (b *LookupBuilder) buildDeletionEmbed() *discord.EmbedBuilder {
	return discord.NewEmbedBuilder().
		SetTitle("🗑️ Data Deletion Notice").
		SetDescription(
			"This user has requested deletion of their data under privacy laws. While we continue to monitor " +
				"server memberships for safety purposes, message history and other details have been redacted.").
		SetColor(constants.ErrorEmbedColor)
}

// buildUserEmbed creates the embed with user information.
func (b *LookupBuilder) buildUserEmbed() *discord.EmbedBuilder {
	userID := b.userID

	username := b.username
	if username == "" {
		username = fmt.Sprintf("Unknown User (%d)", userID)
	}

	embed := discord.NewEmbedBuilder().
		SetTitle("Discord User: "+username).
		SetDescription("Displaying information about this Discord user and their memberships in flagged servers.").
		AddField("User ID", fmt.Sprintf("`%d`", userID), true).
		AddField("Flagged Servers", fmt.Sprintf("`%d`", b.totalGuilds), true).
		AddField("Mention", fmt.Sprintf("<@%d>", userID), true).
		SetColor(constants.DefaultEmbedColor)

	// Add message summary information if available
	if b.messageSummary != nil {
		embed.AddField("Recent Activity", fmt.Sprintf(
			"Total Messages: `%d`\nLast Flagged: <t:%d:R>\nReason: `%s`",
			b.messageSummary.MessageCount,
			b.messageSummary.LastDetected.Unix(),
			b.messageSummary.Reason,
		), false)
	}

	return embed
}

// buildGuildsEmbed creates the embed showing detailed server membership information.
func (b *LookupBuilder) buildGuildsEmbed() *discord.EmbedBuilder {
	embed := discord.NewEmbedBuilder().
		SetTitle("Server Memberships").
		SetColor(constants.DefaultEmbedColor)

	// Check if the user is not a member of any flagged servers
	if len(b.userGuilds) == 0 {
		embed.SetDescription("This user is not a member of any flagged servers in our database.")
		return embed
	}

	// Create a field for each guild
	for _, guild := range b.userGuilds {
		guildName := b.guildNames[guild.ServerID]
		if guildName == "" {
			guildName = constants.UnknownServer
		}

		joinedTimestamp := guild.JoinedAt.Unix()
		content := fmt.Sprintf("Server ID: `%d`\nJoined: <t:%d:R>",
			guild.ServerID,
			joinedTimestamp,
		)

		embed.AddField(guildName, content, false)
	}

	// Add pagination info if available
	if len(b.userGuilds) > 0 {
		embed.SetFooterText(fmt.Sprintf("Showing %d of %d servers", len(b.userGuilds), b.totalGuilds))
	}

	return embed
}

// buildComponents creates all interactive components for the lookup menu.
func (b *LookupBuilder) buildComponents() []discord.ContainerComponent {
	// Create select menu options for guilds with messages
	var options []discord.StringSelectMenuOption
	for _, guild := range b.userGuilds {
		// Only add option if the guild has messages
		if _, ok := b.messageGuilds[guild.ServerID]; ok {
			guildName := b.guildNames[guild.ServerID]
			if guildName == "" {
				guildName = constants.UnknownServer
			}

			options = append(options, discord.NewStringSelectMenuOption(
				guildName,
				strconv.FormatUint(guild.ServerID, 10),
			).WithDescription("View message history in "+guildName))
		}
	}

	components := []discord.ContainerComponent{
		// Navigation buttons
		discord.NewActionRow(
			discord.NewSecondaryButton("◀️ Back", constants.BackButtonCustomID),
			discord.NewSecondaryButton("⏮️", string(session.ViewerFirstPage)).WithDisabled(!b.hasPrevPage),
			discord.NewSecondaryButton("◀️", string(session.ViewerPrevPage)).WithDisabled(!b.hasPrevPage),
			discord.NewSecondaryButton("▶️", string(session.ViewerNextPage)).WithDisabled(!b.hasNextPage),
			discord.NewSecondaryButton("⏭️", string(session.ViewerLastPage)).WithDisabled(true), // This is disabled on purpose
		),
		// Refresh button
		discord.NewActionRow(
			discord.NewSecondaryButton("🔄 Refresh", constants.RefreshButtonCustomID),
		),
	}

	// Add select menu if we have options
	if len(options) > 0 {
		components = append([]discord.ContainerComponent{
			discord.NewActionRow(
				discord.NewStringSelectMenu(constants.GuildMessageSelectMenuCustomID, "View Message History", options...),
			),
		}, components...)
	}

	return components
}

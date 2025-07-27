package guild

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/database/types"
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
	var containers []discord.LayoutComponent

	// Add data deletion notice if data is redacted
	if b.isDataRedacted {
		var deletionContent strings.Builder
		deletionContent.WriteString("## 🗑️ Data Deletion Notice\n")
		deletionContent.WriteString("This user has requested deletion of their data under privacy laws. While we may continue to monitor ")
		deletionContent.WriteString("server memberships for safety purposes, message history and other details have been redacted.")

		containers = append(containers, discord.NewContainer(
			discord.NewTextDisplay(deletionContent.String()),
		).WithAccentColor(constants.ErrorContainerColor))
	}

	// Build user info container
	var userContent strings.Builder

	username := b.username
	if username == "" {
		username = fmt.Sprintf("Unknown User (%d)", b.userID)
	}

	userContent.WriteString(fmt.Sprintf("## Discord User: %s\n", username))
	userContent.WriteString("Displaying information about this Discord user and their memberships in flagged servers.\n")
	userContent.WriteString(fmt.Sprintf("### User ID\n`%d`\n", b.userID))
	userContent.WriteString(fmt.Sprintf("### Flagged Servers\n`%d`\n", b.totalGuilds))
	userContent.WriteString(fmt.Sprintf("### Mention\n<@%d>\n", b.userID))

	// Add message summary information if available
	if b.messageSummary != nil {
		userContent.WriteString("### Recent Activity\n")
		userContent.WriteString(fmt.Sprintf("Total Messages: `%d`\n", b.messageSummary.MessageCount))
		userContent.WriteString(fmt.Sprintf("Last Flagged: <t:%d:R>\n", b.messageSummary.LastDetected.Unix()))
		userContent.WriteString(fmt.Sprintf("Reason: `%s`", b.messageSummary.Reason))
	}

	containers = append(containers, discord.NewContainer(
		discord.NewTextDisplay(userContent.String()),
	).WithAccentColor(constants.DefaultContainerColor))

	// Add guilds container
	containers = append(containers, b.buildGuildsDisplay())

	// Create message update builder with containers and back button
	return discord.NewMessageUpdateBuilder().
		AddComponents(containers...).
		AddComponents(discord.NewActionRow(
			discord.NewSecondaryButton("◀️ Back", constants.BackButtonCustomID),
		))
}

// buildGuildsDisplay creates the container showing guild memberships.
func (b *LookupBuilder) buildGuildsDisplay() discord.LayoutComponent {
	var content strings.Builder
	content.WriteString("## Server Memberships\n\n")

	if len(b.userGuilds) == 0 {
		content.WriteString("This user is not a member of any flagged servers in our database.")

		return discord.NewContainer(
			discord.NewTextDisplay(content.String()),
		).WithAccentColor(constants.DefaultContainerColor)
	}

	// Create sections for each guild
	components := make([]discord.ContainerSubComponent, 0, len(b.userGuilds))
	for _, guild := range b.userGuilds {
		guildName := b.guildNames[guild.ServerID]
		if guildName == "" {
			guildName = constants.UnknownServer
		}

		var guildContent strings.Builder
		guildContent.WriteString(fmt.Sprintf("### %s\n", guildName))
		guildContent.WriteString(fmt.Sprintf("Server ID: `%d`\n", guild.ServerID))

		joinedInfo := "Unknown"
		if !guild.JoinedAt.IsZero() {
			joinedInfo = fmt.Sprintf("<t:%d:R>", guild.JoinedAt.Unix())
		}

		guildContent.WriteString("Joined: " + joinedInfo)

		// Create section for guilds with messages, text display for others
		if _, hasMessages := b.messageGuilds[guild.ServerID]; hasMessages {
			section := discord.NewSection(
				discord.NewTextDisplay(guildContent.String()),
			).WithAccessory(
				discord.NewSecondaryButton("View Messages", strconv.FormatUint(guild.ServerID, 10)),
			)
			components = append(components, section)
		} else {
			components = append(components, discord.NewTextDisplay(guildContent.String()))
		}
	}

	// Add server count at the bottom
	if len(b.userGuilds) > 0 {
		components = append(components, discord.NewTextDisplay(fmt.Sprintf("\n-# Showing %d of %d servers", len(b.userGuilds), b.totalGuilds)))
	}

	// Add pagination buttons
	if b.hasNextPage || b.hasPrevPage {
		components = append(components,
			discord.NewLargeSeparator(),
			discord.NewActionRow(
				discord.NewSecondaryButton("⏮️", string(session.ViewerFirstPage)).WithDisabled(!b.hasPrevPage),
				discord.NewSecondaryButton("◀️", string(session.ViewerPrevPage)).WithDisabled(!b.hasPrevPage),
				discord.NewSecondaryButton("▶️", string(session.ViewerNextPage)).WithDisabled(!b.hasNextPage),
				discord.NewSecondaryButton("⏭️", string(session.ViewerLastPage)).WithDisabled(true), // This is disabled on purpose
			),
		)
	}

	// Create container with all components
	container := discord.NewContainer(
		discord.NewTextDisplay(content.String()),
	).AddComponents(components...)

	return container.WithAccentColor(constants.DefaultContainerColor)
}

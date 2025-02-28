package guild

import (
	"fmt"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/utils"
	"github.com/robalyx/rotector/internal/common/storage/database/types"
)

// ScanBuilder creates the visual layout for the guild scan interface.
type ScanBuilder struct {
	userGuilds      map[uint64][]*types.UserGuildInfo
	filteredUsers   map[uint64][]*types.UserGuildInfo
	guildNames      map[uint64]string
	topGuilds       []*types.GuildCount
	page            int
	total           int
	minGuilds       int
	minJoinDuration time.Duration
}

// NewScanBuilder creates a new scan builder.
func NewScanBuilder(s *session.Session) *ScanBuilder {
	return &ScanBuilder{
		userGuilds:      session.GuildScanUserGuilds.Get(s),
		filteredUsers:   session.GuildScanFilteredUsers.Get(s),
		guildNames:      session.GuildScanGuildNames.Get(s),
		topGuilds:       session.GuildScanTopGuilds.Get(s),
		page:            session.PaginationPage.Get(s),
		total:           session.PaginationTotalItems.Get(s),
		minGuilds:       session.GuildScanMinGuilds.Get(s),
		minJoinDuration: session.GuildScanMinJoinDuration.Get(s),
	}
}

// Build creates a Discord message with the guild scan interface.
func (b *ScanBuilder) Build() *discord.MessageUpdateBuilder {
	// Calculate total pages
	totalPages := max(1, (b.total+constants.GuildScanUsersPerPage-1)/constants.GuildScanUsersPerPage)

	// Build the embeds
	infoEmbed := b.buildInfoEmbed()
	summaryEmbed := b.buildSummaryEmbed()
	resultsEmbed := b.buildResultsEmbed(totalPages)

	// Build the components
	components := b.buildComponents(totalPages)

	return discord.NewMessageUpdateBuilder().
		SetEmbeds(infoEmbed.Build(), summaryEmbed.Build(), resultsEmbed.Build()).
		AddContainerComponents(components...)
}

// buildInfoEmbed creates the embed showing active filters and requirements.
func (b *ScanBuilder) buildInfoEmbed() *discord.EmbedBuilder {
	description := fmt.Sprintf(
		"These filters determine which users will be included in the ban operation.\n\n"+
			"Total flagged users: `%d`\n"+
			"Users meeting filter criteria: `%d`",
		len(b.userGuilds),
		len(b.filteredUsers),
	)

	filterEmbed := discord.NewEmbedBuilder().
		SetTitle("Banning Users").
		SetDescription(description).
		SetColor(0x3498DB) // Blue color for filter embed

	// Add guilds filter information
	filterText := fmt.Sprintf("Minimum Guilds: `%d` (Users must appear in at least this many flagged guilds)", b.minGuilds)

	// Add join duration information if set
	minJoinDuration := b.minJoinDuration
	if minJoinDuration > 0 {
		filterText += fmt.Sprintf("\nMinimum Join Duration: `%s` (Users must be in guilds for at least this long)", utils.FormatDuration(minJoinDuration))
	}

	filterEmbed.AddField(
		"Filters",
		filterText,
		false,
	)

	// Add permission requirements
	filterEmbed.AddField(
		"Requirements",
		"- You must have **Administrator** permission\n"+
			"- The bot must have **Ban Members** permission\n"+
			"- The bot's role must be higher than targeted users",
		false,
	)

	return filterEmbed
}

// buildSummaryEmbed creates the embed showing summary statistics about flagged guilds.
func (b *ScanBuilder) buildSummaryEmbed() *discord.EmbedBuilder {
	summaryEmbed := discord.NewEmbedBuilder().
		SetTitle("Guilds Summary").
		SetDescription("Most common flagged guilds found in this scan.").
		SetColor(0xE67E22) // Orange color for summary embed

	// Add top 5 guilds to summary embed
	if len(b.topGuilds) > 0 {
		// Show top 5 guilds
		topCount := min(5, len(b.topGuilds))

		summaryContent := ""
		for i := range topCount {
			gc := b.topGuilds[i]
			guildName := b.guildNames[gc.ServerID]
			if guildName == "" {
				guildName = "Unknown Server"
			}

			summaryContent += fmt.Sprintf("- **%s**: %d users\n", guildName, gc.Count)
		}

		summaryEmbed.AddField("Top Flagged Guilds", summaryContent, false)

		// Add recommendation
		if b.minGuilds == 1 && len(b.topGuilds) > 3 {
			summaryEmbed.AddField(
				"Recommendation",
				"Consider increasing the minimum guilds filter to reduce false positives. Users in multiple flagged guilds are more likely to be inappropriate.",
				false,
			)
		}
	} else {
		summaryEmbed.AddField("No Flagged Guilds Found", "No users match the current filter criteria.", false)
	}

	return summaryEmbed
}

// buildResultsEmbed creates the embed showing detailed user results for the current page.
func (b *ScanBuilder) buildResultsEmbed(totalPages int) *discord.EmbedBuilder {
	resultsEmbed := discord.NewEmbedBuilder().
		SetTitle(fmt.Sprintf("Scan Results (Page %d/%d)", b.page+1, totalPages)).
		SetColor(constants.DefaultEmbedColor)

	// Only add user fields if there are filtered results
	if len(b.filteredUsers) > 0 {
		// Add paginated user results
		b.addPaginatedUserResults(resultsEmbed)

		// Add warning footer
		resultsEmbed.SetFooter("⚠️ Review the list carefully before confirming bans", "")
	} else {
		resultsEmbed.AddField(
			"No Users Match Filter Criteria",
			"Adjust your filters to include more users in the ban operation.",
			false,
		)
	}

	return resultsEmbed
}

// addPaginatedUserResults adds user entries to the results embed for the current page.
func (b *ScanBuilder) addPaginatedUserResults(embed *discord.EmbedBuilder) {
	// Convert map to slice for pagination
	type userEntry struct {
		userID uint64
		guilds []*types.UserGuildInfo
	}

	users := make([]userEntry, 0, len(b.filteredUsers))
	for userID, guilds := range b.filteredUsers {
		users = append(users, userEntry{
			userID: userID,
			guilds: guilds,
		})
	}

	// Calculate page boundaries
	start := b.page * constants.GuildScanUsersPerPage
	end := min(start+constants.GuildScanUsersPerPage, len(users))

	// Add fields for each user
	for i, entry := range users[start:end] {
		userID := entry.userID
		guilds := entry.guilds

		var guildInfos []string

		// Limit to 5 guilds per user
		maxGuildsToShow := min(5, len(guilds))
		for j := range maxGuildsToShow {
			guild := guilds[j]
			guildName := b.guildNames[guild.ServerID]
			if guildName == "" {
				guildName = "Unknown Server"
			}

			joinedAgo := utils.FormatTimeAgo(guild.JoinedAt)
			guildInfos = append(guildInfos, fmt.Sprintf("• %s (joined %s)",
				guildName,
				joinedAgo,
			))
		}

		// Add note about additional guilds if there are more than 5
		if len(guilds) > 5 {
			guildInfos = append(guildInfos, fmt.Sprintf("- ... and %d more", len(guilds)-5))
		}

		// Create content for field
		content := fmt.Sprintf("<@%d>\nFound in %d servers:\n```md\n%s\n```",
			userID,
			len(guilds),
			strings.Join(guildInfos, "\n"),
		)

		embed.AddField(
			fmt.Sprintf("User %d", start+i+1),
			content,
			false,
		)
	}
}

// buildComponents creates the interactive components for the scan interface.
func (b *ScanBuilder) buildComponents(totalPages int) []discord.ContainerComponent {
	// Add join duration option with appropriate description
	joinDurationDesc := "Not set"
	if b.minJoinDuration > 0 {
		joinDurationDesc = "Current: " + utils.FormatDuration(b.minJoinDuration)
	}

	// Create filter dropdown options
	filterOptions := []discord.StringSelectMenuOption{
		discord.NewStringSelectMenuOption("Minimum Guilds", constants.GuildScanMinGuildsOption).
			WithDescription(fmt.Sprintf("Current: %d", b.minGuilds)),
		discord.NewStringSelectMenuOption("Minimum Join Duration", constants.GuildScanJoinDurationOption).
			WithDescription(joinDurationDesc),
	}

	return []discord.ContainerComponent{
		// Filter dropdown
		discord.NewActionRow(
			discord.NewStringSelectMenu(constants.GuildScanFilterSelectMenuCustomID, "Set Filter Conditions", filterOptions...),
		),
		// Pagination buttons
		discord.NewActionRow(
			discord.NewSecondaryButton("◀️ Back", constants.BackButtonCustomID),
			discord.NewSecondaryButton("⏮️", string(session.ViewerFirstPage)).WithDisabled(b.page == 0),
			discord.NewSecondaryButton("◀️", string(session.ViewerPrevPage)).WithDisabled(b.page == 0),
			discord.NewSecondaryButton("▶️", string(session.ViewerNextPage)).WithDisabled(b.page == totalPages-1),
			discord.NewSecondaryButton("⏭️", string(session.ViewerLastPage)).WithDisabled(b.page == totalPages-1),
		),
		// Action buttons
		discord.NewActionRow(
			discord.NewDangerButton("Reset Filters", constants.ClearFiltersButtonCustomID),
			discord.NewDangerButton("Confirm Bans", constants.ConfirmGuildBansButtonCustomID).
				WithDisabled(len(b.filteredUsers) == 0),
		),
	}
}

package guild

import (
	"fmt"
	"sort"
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
	scanType          string
	userGuilds        map[uint64][]*types.UserGuildInfo
	messageSummaries  map[uint64]*types.InappropriateUserSummary
	filteredUsers     map[uint64][]*types.UserGuildInfo
	filteredSummaries map[uint64]*types.InappropriateUserSummary
	guildNames        map[uint64]string
	page              int
	total             int
	minGuilds         int
	minJoinDuration   time.Duration
}

// NewScanBuilder creates a new scan builder.
func NewScanBuilder(s *session.Session) *ScanBuilder {
	return &ScanBuilder{
		scanType:          session.GuildScanType.Get(s),
		userGuilds:        session.GuildScanUserGuilds.Get(s),
		messageSummaries:  session.GuildScanMessageSummaries.Get(s),
		filteredUsers:     session.GuildScanFilteredUsers.Get(s),
		filteredSummaries: session.GuildScanFilteredSummaries.Get(s),
		guildNames:        session.GuildScanGuildNames.Get(s),
		page:              session.PaginationPage.Get(s),
		total:             session.PaginationTotalItems.Get(s),
		minGuilds:         session.GuildScanMinGuilds.Get(s),
		minJoinDuration:   session.GuildScanMinJoinDuration.Get(s),
	}
}

// Build creates a Discord message with the guild scan interface.
func (b *ScanBuilder) Build() *discord.MessageUpdateBuilder {
	// Calculate total pages
	totalPages := max(1, (b.total+constants.GuildScanUsersPerPage-1)/constants.GuildScanUsersPerPage)

	// Build the embeds
	warningEmbed := b.buildWarningEmbed()
	infoEmbed := b.buildInfoEmbed()
	resultsEmbed := b.buildResultsEmbed(totalPages)

	// Build the components
	components := b.buildComponents(totalPages)

	return discord.NewMessageUpdateBuilder().
		SetEmbeds(warningEmbed.Build(), infoEmbed.Build(), resultsEmbed.Build()).
		AddContainerComponents(components...)
}

// buildInfoEmbed creates the embed showing active filters and requirements.
func (b *ScanBuilder) buildInfoEmbed() *discord.EmbedBuilder {
	var totalUsers int
	var filteredUsers int

	if b.scanType == constants.GuildScanTypeMessages {
		totalUsers = len(b.messageSummaries)
		filteredUsers = len(b.filteredSummaries)
	} else {
		totalUsers = len(b.userGuilds)
		filteredUsers = len(b.filteredUsers)
	}

	description := fmt.Sprintf(
		"Filters determine which users will be included in the ban operation.\n\n"+
			"Total flagged users: `%d`\n"+
			"Users meeting filter criteria: `%d`",
		totalUsers,
		filteredUsers,
	)

	filterEmbed := discord.NewEmbedBuilder().
		SetTitle("Banning Users").
		SetDescription(description).
		SetColor(0x3498DB) // Blue color for filter embed

	// Build filter text
	var filterParts []string

	// Add guilds filter information
	if b.scanType == constants.GuildScanTypeCondo && b.minGuilds > 1 {
		filterParts = append(filterParts, fmt.Sprintf(
			"Minimum Guilds: `%d` (Users must appear in at least this many flagged guilds)",
			b.minGuilds,
		))
	}

	// Add join duration information
	if b.minJoinDuration > 0 {
		if b.scanType == constants.GuildScanTypeMessages {
			filterParts = append(filterParts, fmt.Sprintf(
				"Minimum Message Age: `%s` (Only show messages older than this)",
				utils.FormatDuration(b.minJoinDuration),
			))
		} else {
			filterParts = append(filterParts, fmt.Sprintf(
				"Minimum Join Duration: `%s` (Users must be in guilds for at least this long)",
				utils.FormatDuration(b.minJoinDuration),
			))
		}
	}

	// Add filters field if we have any active filters
	if len(filterParts) > 0 {
		filterEmbed.AddField(
			"Active Filters",
			strings.Join(filterParts, "\n"),
			false,
		)
	}

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

// buildWarningEmbed creates the warning embed based on scan type.
func (b *ScanBuilder) buildWarningEmbed() *discord.EmbedBuilder {
	warningEmbed := discord.NewEmbedBuilder()

	if b.scanType == constants.GuildScanTypeCondo {
		// Warning for condo server scan
		warningEmbed.SetTitle("⚠️ Warning: Unreliable Method").
			SetDescription(
				"This method may result in false positives as some legitimate users might "+
					"be included like investigators, reporters, non-participating members, accounts "+
					"that were compromised, and users who joined through misleading invites.\n\n"+
					"This is one of the known flaws of Ruben's Ro-Cleaner bot but we allow you to "+
					"reduce the number of false positives by setting certain filters.\n\n"+
					"However, we **recommend using the 'Ban Users with Inappropriate Messages' option** "+
					"for 100% accurate results.",
			).
			SetColor(0xFF0000). // Red color for warning
			AddField(
				"Flagging Conditions",
				"- Sending messages in inappropriate servers (active participation)\n"+
					"- Being present in inappropriate servers for over 12 hours (beyond grace period)",
				false,
			).
			AddField(
				"Important Notes",
				"- Users are automatically removed if they've been clean for 7 days\n"+
					"- The grace period helps exclude users who join and leave quickly",
				false,
			).
			SetFooter("⚠️ Review the list carefully before confirming bans", "")

		// Add recommendation if there are many flagged guilds
		if b.minGuilds == 1 && len(b.guildNames) > 3 {
			warningEmbed.AddField(
				"Filter Recommendation",
				"Consider increasing the minimum guilds filter to reduce false positives. "+
					"Users in multiple flagged servers are more likely to be inappropriate.",
				false,
			)
		}
	} else {
		// Info for message scan
		warningEmbed.SetTitle("💬 Message-Based Scan").
			SetDescription("You are using the recommended message-based scan method.\n\n" +
				"This scan identifies users based on their actual inappropriate messages, " +
						"providing more accurate results than server membership alone.").
			SetColor(0x00FF00) // Green color
	}

	return warningEmbed
}

// buildResultsEmbed creates the embed showing detailed user results for the current page.
func (b *ScanBuilder) buildResultsEmbed(totalPages int) *discord.EmbedBuilder {
	resultsEmbed := discord.NewEmbedBuilder().
		SetTitle(fmt.Sprintf("Scan Results (Page %d/%d)", b.page+1, totalPages)).
		SetColor(constants.DefaultEmbedColor)

	if b.scanType == constants.GuildScanTypeMessages {
		// Show message scan results
		if len(b.filteredSummaries) > 0 {
			b.addPaginatedMessageResults(resultsEmbed)
			return resultsEmbed
		}
	} else if len(b.filteredUsers) > 0 {
		// Show condo scan results
		b.addPaginatedUserResults(resultsEmbed)
		return resultsEmbed
	}

	resultsEmbed.AddField(
		"No Users Match Filter Criteria",
		"Adjust your filters to include more users in the ban operation.",
		false,
	)

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
				guildName = constants.UnknownServer
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

// addPaginatedMessageResults adds message summary entries to the results embed for the current page.
func (b *ScanBuilder) addPaginatedMessageResults(embed *discord.EmbedBuilder) {
	// Convert map to slice for pagination
	type summaryEntry struct {
		userID  uint64
		summary *types.InappropriateUserSummary
	}

	summaries := make([]summaryEntry, 0, len(b.filteredSummaries))
	for userID, summary := range b.filteredSummaries {
		summaries = append(summaries, summaryEntry{
			userID:  userID,
			summary: summary,
		})
	}

	// Sort by message count descending
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].summary.MessageCount > summaries[j].summary.MessageCount
	})

	// Calculate page boundaries
	start := b.page * constants.GuildScanUsersPerPage
	end := min(start+constants.GuildScanUsersPerPage, len(summaries))

	// Add fields for each user
	for i, entry := range summaries[start:end] {
		content := fmt.Sprintf("<@%d>\nInappropriate Messages: `%d`\nLast Detected: <t:%d:R>\nReason: `%s`",
			entry.userID,
			entry.summary.MessageCount,
			entry.summary.LastDetected.Unix(),
			entry.summary.Reason,
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
	var filterOptions []discord.StringSelectMenuOption

	if b.scanType == constants.GuildScanTypeMessages {
		// Message scan filters
		filterOptions = []discord.StringSelectMenuOption{
			discord.NewStringSelectMenuOption("Minimum Message Age", constants.GuildScanJoinDurationOption).
				WithDescription(joinDurationDesc),
		}
	} else {
		// Condo scan filters
		filterOptions = []discord.StringSelectMenuOption{
			discord.NewStringSelectMenuOption("Minimum Guilds", constants.GuildScanMinGuildsOption).
				WithDescription(fmt.Sprintf("Current: %d", b.minGuilds)),
			discord.NewStringSelectMenuOption("Minimum Join Duration", constants.GuildScanJoinDurationOption).
				WithDescription(joinDurationDesc),
		}
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
				WithDisabled(func() bool {
					if b.scanType == constants.GuildScanTypeMessages {
						return len(b.filteredSummaries) == 0
					}
					return len(b.filteredUsers) == 0
				}()),
		),
	}
}

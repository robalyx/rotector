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
	"github.com/robalyx/rotector/internal/database/types"
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

	// Build the containers
	containers := []discord.LayoutComponent{
		b.buildWarningDisplay(),
		b.buildInfoDisplay(),
		b.buildResultsDisplay(totalPages),
	}

	// Build interactive components
	components := b.buildInteractiveComponents(totalPages)

	return discord.NewMessageUpdateBuilder().
		AddComponents(containers...).
		AddComponents(components...)
}

// buildWarningDisplay creates the warning container based on scan type.
func (b *ScanBuilder) buildWarningDisplay() discord.LayoutComponent {
	var content strings.Builder

	if b.scanType == constants.GuildScanTypeCondo {
		// Warning for condo server scan
		content.WriteString("## ⚠️ Warning: Unreliable Method\n")
		content.WriteString("This method may result in false positives as some legitimate users might ")
		content.WriteString("be included like investigators, reporters, non-participating members, accounts ")
		content.WriteString("that were compromised, and users who joined through misleading invites.\n\n")
		content.WriteString("This is one of the known flaws of Ruben's Ro-Cleaner bot but we allow you to ")
		content.WriteString("reduce the number of false positives by setting certain filters.\n\n")
		content.WriteString("However, we **recommend using the 'Ban Users with Inappropriate Messages' option** ")
		content.WriteString("for higher accuracy results.\n")

		content.WriteString("### Flagging Conditions\n")
		content.WriteString("- Sending messages in inappropriate servers (active participation)\n")
		content.WriteString("- Being present in inappropriate servers for over 12 hours (beyond grace period)\n")

		content.WriteString("### Important Notes\n")
		content.WriteString("- Users are automatically removed if they've been clean for 7 days\n")
		content.WriteString("- The grace period helps exclude users who join and leave quickly\n\n")

		content.WriteString("Review the list carefully before confirming bans!\n")

		// Add recommendation if there are many flagged guilds
		if b.minGuilds == 1 && len(b.guildNames) > 3 {
			content.WriteString("\n### Filter Recommendation\n")
			content.WriteString("Consider increasing the minimum guilds filter to reduce false positives. ")
			content.WriteString("Users in multiple flagged servers are more likely to be inappropriate.")
		}

		return discord.NewContainer(
			discord.NewTextDisplay(content.String()),
		).WithAccentColor(constants.ErrorContainerColor)
	}

	// Info for message scan
	content.WriteString("## 💬 Message-Based Scan\n")
	content.WriteString("You are using the recommended message-based scan method.\n\n")
	content.WriteString("This scan identifies users based on their actual inappropriate messages, ")
	content.WriteString("providing more accurate results than server membership alone.")

	return discord.NewContainer(
		discord.NewTextDisplay(content.String()),
	).WithAccentColor(constants.DefaultContainerColor)
}

// buildInfoDisplay creates the container showing active filters and requirements.
func (b *ScanBuilder) buildInfoDisplay() discord.LayoutComponent {
	var (
		content       strings.Builder
		totalUsers    int
		filteredUsers int
	)

	if b.scanType == constants.GuildScanTypeMessages {
		totalUsers = len(b.messageSummaries)
		filteredUsers = len(b.filteredSummaries)
	} else {
		totalUsers = len(b.userGuilds)
		filteredUsers = len(b.filteredUsers)
	}

	content.WriteString("## Banning Users\n")
	content.WriteString("Filters determine which users will be included in the ban operation.\n\n")
	content.WriteString(fmt.Sprintf("Total flagged users: `%d`\n", totalUsers))
	content.WriteString(fmt.Sprintf("Users meeting filter criteria: `%d`\n", filteredUsers))

	// Add filter information
	if b.scanType == constants.GuildScanTypeCondo && b.minGuilds > 1 ||
		b.minJoinDuration > 0 {
		content.WriteString("### Active Filters\n")

		if b.scanType == constants.GuildScanTypeCondo && b.minGuilds > 1 {
			content.WriteString(fmt.Sprintf("- Minimum Guilds: `%d` (Users must appear in at least this many flagged guilds)\n",
				b.minGuilds))
		}

		if b.minJoinDuration > 0 {
			if b.scanType == constants.GuildScanTypeMessages {
				content.WriteString(fmt.Sprintf("- Minimum Message Age: `%s` (Only show messages older than this)\n",
					utils.FormatDuration(b.minJoinDuration)))
			} else {
				content.WriteString(fmt.Sprintf("- Minimum Join Duration: `%s` (Users must be in guilds for at least this long)\n",
					utils.FormatDuration(b.minJoinDuration)))
			}
		}
	}

	// Add requirements
	content.WriteString("### Requirements\n")
	content.WriteString("- You must have **Administrator** permission\n")
	content.WriteString("- The bot must have **Ban Members** permission\n")
	content.WriteString("- The bot's role must be higher than targeted users")

	return discord.NewContainer(
		discord.NewTextDisplay(content.String()),
	).WithAccentColor(constants.DefaultContainerColor)
}

// buildResultsDisplay creates the container showing detailed user results for the current page.
func (b *ScanBuilder) buildResultsDisplay(totalPages int) discord.LayoutComponent {
	var content strings.Builder
	content.WriteString(fmt.Sprintf("## Scan Results (Page %d/%d)\n", b.page+1, totalPages))

	switch b.scanType {
	case constants.GuildScanTypeMessages:
		// Show message scan results
		if len(b.filteredSummaries) > 0 {
			b.addPaginatedMessageResults(&content)
		} else {
			content.WriteString("### No Users Match Filter Criteria\n")
			content.WriteString("Adjust your filters to include more users in the ban operation.")
		}
	case constants.GuildScanTypeCondo:
		// Show condo scan results
		if len(b.filteredUsers) > 0 {
			b.addPaginatedUserResults(&content)
		} else {
			content.WriteString("### No Users Match Filter Criteria\n")
			content.WriteString("Adjust your filters to include more users in the ban operation.")
		}
	}

	return discord.NewContainer(
		discord.NewTextDisplay(content.String()),
	).WithAccentColor(constants.DefaultContainerColor)
}

// addPaginatedUserResults adds user entries to the results content for the current page.
func (b *ScanBuilder) addPaginatedUserResults(content *strings.Builder) {
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

	// Add entries for each user
	for i, entry := range users[start:end] {
		userID := entry.userID
		guilds := entry.guilds

		fmt.Fprintf(content, "### User %d\n", start+i+1)
		fmt.Fprintf(content, "<@%d>\n", userID)
		fmt.Fprintf(content, "Found in %d servers:\n```md\n", len(guilds))

		// Limit to 5 guilds per user
		maxGuildsToShow := min(5, len(guilds))
		for j := range maxGuildsToShow {
			guild := guilds[j]

			guildName := b.guildNames[guild.ServerID]
			if guildName == "" {
				guildName = constants.UnknownServer
			}

			joinedAgo := utils.FormatTimeAgo(guild.JoinedAt)
			fmt.Fprintf(content, "• %s (joined %s)\n",
				guildName,
				joinedAgo)
		}

		// Add note about additional guilds if there are more than 5
		if len(guilds) > 5 {
			fmt.Fprintf(content, "- ... and %d more", len(guilds)-5)
		}

		content.WriteString("\n```\n")
	}
}

// addPaginatedMessageResults adds message summary entries to the results content for the current page.
func (b *ScanBuilder) addPaginatedMessageResults(content *strings.Builder) {
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

	// Add entries for each user
	for i, entry := range summaries[start:end] {
		fmt.Fprintf(content, "### User %d\n", start+i+1)
		fmt.Fprintf(content, "<@%d>\n", entry.userID)
		fmt.Fprintf(content, "Inappropriate Messages: `%d`\n", entry.summary.MessageCount)
		fmt.Fprintf(content, "Last Detected: <t:%d:R>\n", entry.summary.LastDetected.Unix())
		fmt.Fprintf(content, "Reason: `%s`\n\n", entry.summary.Reason)
	}
}

// buildInteractiveComponents creates the interactive components for the scan interface.
func (b *ScanBuilder) buildInteractiveComponents(totalPages int) []discord.LayoutComponent {
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

	return []discord.LayoutComponent{
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

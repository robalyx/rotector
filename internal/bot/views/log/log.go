package log

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/robalyx/rotector/internal/bot/constants"
	"github.com/robalyx/rotector/internal/bot/core/session"
	"github.com/robalyx/rotector/internal/bot/utils"
	"github.com/robalyx/rotector/internal/database/types"
	"github.com/robalyx/rotector/internal/database/types/enum"
)

// Builder creates the visual layout for viewing activity logs.
type Builder struct {
	logs               []*types.ActivityLog
	guildID            uint64
	discordID          uint64
	userID             int64
	groupID            int64
	reviewerID         uint64
	activityTypeFilter enum.ActivityType
	categoryFilter     string
	startDate          time.Time
	endDate            time.Time
	hasNextPage        bool
	hasPrevPage        bool
	privacyMode        bool
}

// NewBuilder creates a new log builder.
func NewBuilder(s *session.Session) *Builder {
	return &Builder{
		logs:               session.LogActivities.Get(s),
		guildID:            session.LogFilterGuildID.Get(s),
		discordID:          session.LogFilterDiscordID.Get(s),
		userID:             session.LogFilterUserID.Get(s),
		groupID:            session.LogFilterGroupID.Get(s),
		reviewerID:         session.LogFilterReviewerID.Get(s),
		activityTypeFilter: session.LogFilterActivityType.Get(s),
		categoryFilter:     session.LogFilterActivityCategory.Get(s),
		startDate:          session.LogFilterDateRangeStart.Get(s),
		endDate:            session.LogFilterDateRangeEnd.Get(s),
		hasNextPage:        session.PaginationHasNextPage.Get(s),
		hasPrevPage:        session.PaginationHasPrevPage.Get(s),
		privacyMode:        session.UserReviewMode.Get(s) == enum.ReviewModeTraining || session.UserStreamerMode.Get(s),
	}
}

// Build creates a Discord message showing the log entries.
func (b *Builder) Build() *discord.MessageUpdateBuilder {
	builder := discord.NewMessageUpdateBuilder()

	// Create main info container
	var mainContent strings.Builder
	mainContent.WriteString("## Log Query Results\n\n")

	// Add fields for each active query condition
	if b.guildID != 0 {
		mainContent.WriteString(fmt.Sprintf("Guild ID: `%s`\n",
			utils.CensorString(strconv.FormatUint(b.guildID, 10), b.privacyMode)))
	}

	if b.discordID != 0 {
		mainContent.WriteString(fmt.Sprintf("Discord ID: `%s`\n",
			utils.CensorString(strconv.FormatUint(b.discordID, 10), b.privacyMode)))
	}

	if b.userID != 0 {
		mainContent.WriteString(fmt.Sprintf("User ID: `%s`\n",
			utils.CensorString(strconv.FormatInt(b.userID, 10), b.privacyMode)))
	}

	if b.groupID != 0 {
		mainContent.WriteString(fmt.Sprintf("Group ID: `%s`\n",
			utils.CensorString(strconv.FormatInt(b.groupID, 10), b.privacyMode)))
	}

	if b.reviewerID != 0 {
		mainContent.WriteString(fmt.Sprintf("Reviewer ID: `%d`\n", b.reviewerID))
	}

	if b.activityTypeFilter != enum.ActivityTypeAll {
		mainContent.WriteString(fmt.Sprintf("Activity Type: `%s`\n", b.activityTypeFilter))
	}

	if !b.startDate.IsZero() && !b.endDate.IsZero() {
		mainContent.WriteString(fmt.Sprintf("Date Range: `%s` to `%s`\n",
			b.startDate.Format("2006-01-02"), b.endDate.Format("2006-01-02")))
	}

	mainContainer := discord.NewContainer(
		discord.NewTextDisplay(mainContent.String()),
	).WithAccentColor(utils.GetContainerColor(b.privacyMode))

	builder.AddComponents(mainContainer)

	// Create logs container with content and interactive components
	var components []discord.ContainerSubComponent

	// Add log entries if any exist
	if len(b.logs) > 0 {
		var logsContent strings.Builder
		logsContent.WriteString("## Activity Logs\n\n")

		for _, log := range b.logs {
			// Format timestamp as header
			logsContent.WriteString(fmt.Sprintf("### <t:%d:F>\n", log.ActivityTimestamp.Unix()))

			// Add activity type
			logsContent.WriteString(fmt.Sprintf("Activity: `%s`\n", log.ActivityType.String()))

			// Add target information
			if log.ActivityTarget.GuildID != 0 {
				logsContent.WriteString(fmt.Sprintf("Guild: %d\n", log.ActivityTarget.GuildID))
			}

			if log.ActivityTarget.DiscordID != 0 {
				logsContent.WriteString(fmt.Sprintf("Discord: <@%d>\n", log.ActivityTarget.DiscordID))
			}

			if log.ActivityTarget.UserID != 0 {
				logsContent.WriteString(fmt.Sprintf("User: [%s](https://www.roblox.com/users/%d/profile)\n",
					utils.CensorString(strconv.FormatInt(log.ActivityTarget.UserID, 10), b.privacyMode),
					log.ActivityTarget.UserID))
			}

			if log.ActivityTarget.GroupID != 0 {
				logsContent.WriteString(fmt.Sprintf("Group: [%s](https://www.roblox.com/communities/%d)\n",
					utils.CensorString(strconv.FormatInt(log.ActivityTarget.GroupID, 10), b.privacyMode),
					log.ActivityTarget.GroupID))
			}

			// Add reviewer and details
			logsContent.WriteString(fmt.Sprintf("Reviewer: <@%d>\n", log.ReviewerID))

			for key, value := range log.Details {
				newKey := strings.ToUpper(key[:1]) + key[1:]
				newValue := utils.NormalizeString(fmt.Sprintf("%v", value))
				truncatedValue := utils.TruncateString(newValue, 200)
				logsContent.WriteString(fmt.Sprintf("%s: `%s`\n", newKey, truncatedValue))
			}
		}

		// Add footer text
		logsContent.WriteString(fmt.Sprintf("\n-# Sequence %d | %d logs shown\n", b.logs[0].Sequence, len(b.logs)))

		components = append(components, discord.NewTextDisplay(logsContent.String()))
	} else {
		components = append(components, discord.NewTextDisplay("## No Results\nNo log entries found for the given query"))
	}

	// Add separator and interactive components
	components = append(components, discord.NewLargeSeparator())
	components = append(components, b.buildInteractiveComponents()...)

	logsContainer := discord.NewContainer(components...).WithAccentColor(utils.GetContainerColor(b.privacyMode))
	builder.AddComponents(logsContainer)

	// Add back button outside containers
	builder.AddComponents(discord.NewActionRow(
		discord.NewSecondaryButton("◀️ Back", constants.BackButtonCustomID),
	))

	return builder
}

// buildInteractiveComponents creates the interactive components for the log view.
func (b *Builder) buildInteractiveComponents() []discord.ContainerSubComponent {
	return []discord.ContainerSubComponent{
		// Query condition selection menu
		discord.NewActionRow(
			discord.NewStringSelectMenu(constants.ActionSelectMenuCustomID, "Set Filter Condition",
				discord.NewStringSelectMenuOption("Filter by Guild ID", constants.LogsQueryGuildIDOption).
					WithDescription(utils.CensorString(strconv.FormatUint(b.guildID, 10), b.privacyMode)),
				discord.NewStringSelectMenuOption("Filter by Discord ID", constants.LogsQueryDiscordIDOption).
					WithDescription(utils.CensorString(strconv.FormatUint(b.discordID, 10), b.privacyMode)),
				discord.NewStringSelectMenuOption("Filter by User ID", constants.LogsQueryUserIDOption).
					WithDescription(utils.CensorString(strconv.FormatInt(b.userID, 10), b.privacyMode)),
				discord.NewStringSelectMenuOption("Filter by Group ID", constants.LogsQueryGroupIDOption).
					WithDescription(utils.CensorString(strconv.FormatInt(b.groupID, 10), b.privacyMode)),
				discord.NewStringSelectMenuOption("Filter by Reviewer ID", constants.LogsQueryReviewerIDOption).
					WithDescription(strconv.FormatUint(b.reviewerID, 10)),
				discord.NewStringSelectMenuOption("Filter by Date Range", constants.LogsQueryDateRangeOption).
					WithDescription(fmt.Sprintf("%s to %s", b.startDate.Format("2006-01-02"), b.endDate.Format("2006-01-02"))),
			),
		),
		// Activity type filter menu
		discord.NewActionRow(
			discord.NewStringSelectMenu(constants.LogsQueryActivityTypeFilterCustomID, "Filter Activity Type",
				b.buildActivityTypeOptions()...),
		),
		// Clear filters and refresh buttons
		discord.NewActionRow(
			discord.NewDangerButton("Clear Filters", constants.ClearFiltersButtonCustomID),
			discord.NewSecondaryButton("Refresh Logs", constants.RefreshButtonCustomID),
		),
		// Navigation buttons
		discord.NewActionRow(
			discord.NewSecondaryButton("⏮️", string(session.ViewerFirstPage)).WithDisabled(!b.hasPrevPage),
			discord.NewSecondaryButton("◀️", string(session.ViewerPrevPage)).WithDisabled(!b.hasPrevPage),
			discord.NewSecondaryButton("▶️", string(session.ViewerNextPage)).WithDisabled(!b.hasNextPage),
			discord.NewSecondaryButton("⏭️", string(session.ViewerLastPage)).WithDisabled(true), // This is disabled on purpose
		),
	}
}

// buildActivityTypeOptions creates the options for the activity type filter menu.
func (b *Builder) buildActivityTypeOptions() []discord.StringSelectMenuOption {
	// First option is always "All"
	options := []discord.StringSelectMenuOption{
		discord.NewStringSelectMenuOption("All", strconv.Itoa(int(enum.ActivityTypeAll))).
			WithDefault(b.activityTypeFilter == enum.ActivityTypeAll),
	}

	// Group options by category
	userOptions := []discord.StringSelectMenuOption{
		discord.NewStringSelectMenuOption("User Viewed", strconv.Itoa(int(enum.ActivityTypeUserViewed))).
			WithDefault(b.activityTypeFilter == enum.ActivityTypeUserViewed),
		discord.NewStringSelectMenuOption("User Lookup", strconv.Itoa(int(enum.ActivityTypeUserLookup))).
			WithDefault(b.activityTypeFilter == enum.ActivityTypeUserLookup),
		discord.NewStringSelectMenuOption("User Confirmed", strconv.Itoa(int(enum.ActivityTypeUserConfirmed))).
			WithDefault(b.activityTypeFilter == enum.ActivityTypeUserConfirmed),
		discord.NewStringSelectMenuOption("User Cleared", strconv.Itoa(int(enum.ActivityTypeUserCleared))).
			WithDefault(b.activityTypeFilter == enum.ActivityTypeUserCleared),
		discord.NewStringSelectMenuOption("User Skipped", strconv.Itoa(int(enum.ActivityTypeUserSkipped))).
			WithDefault(b.activityTypeFilter == enum.ActivityTypeUserSkipped),
		discord.NewStringSelectMenuOption("User Queued", strconv.Itoa(int(enum.ActivityTypeUserQueued))).
			WithDefault(b.activityTypeFilter == enum.ActivityTypeUserQueued),
		discord.NewStringSelectMenuOption("User Deleted", strconv.Itoa(int(enum.ActivityTypeUserDeleted))).
			WithDefault(b.activityTypeFilter == enum.ActivityTypeUserDeleted),
	}

	groupOptions := []discord.StringSelectMenuOption{
		discord.NewStringSelectMenuOption("Group Viewed", strconv.Itoa(int(enum.ActivityTypeGroupViewed))).
			WithDefault(b.activityTypeFilter == enum.ActivityTypeGroupViewed),
		discord.NewStringSelectMenuOption("Group Lookup", strconv.Itoa(int(enum.ActivityTypeGroupLookup))).
			WithDefault(b.activityTypeFilter == enum.ActivityTypeGroupLookup),
		discord.NewStringSelectMenuOption("Group Confirmed", strconv.Itoa(int(enum.ActivityTypeGroupConfirmed))).
			WithDefault(b.activityTypeFilter == enum.ActivityTypeGroupConfirmed),
		discord.NewStringSelectMenuOption("Group Confirmed (Custom)", strconv.Itoa(int(enum.ActivityTypeGroupConfirmedCustom))).
			WithDefault(b.activityTypeFilter == enum.ActivityTypeGroupConfirmedCustom),
		discord.NewStringSelectMenuOption("Group Cleared", strconv.Itoa(int(enum.ActivityTypeGroupCleared))).
			WithDefault(b.activityTypeFilter == enum.ActivityTypeGroupCleared),
		discord.NewStringSelectMenuOption("Group Skipped", strconv.Itoa(int(enum.ActivityTypeGroupSkipped))).
			WithDefault(b.activityTypeFilter == enum.ActivityTypeGroupSkipped),
		discord.NewStringSelectMenuOption("Group Deleted", strconv.Itoa(int(enum.ActivityTypeGroupDeleted))).
			WithDefault(b.activityTypeFilter == enum.ActivityTypeGroupDeleted),
	}

	otherOptions := []discord.StringSelectMenuOption{
		discord.NewStringSelectMenuOption("Discord User Banned", strconv.Itoa(int(enum.ActivityTypeDiscordUserBanned))).
			WithDefault(b.activityTypeFilter == enum.ActivityTypeDiscordUserBanned),
		discord.NewStringSelectMenuOption("Discord User Unbanned", strconv.Itoa(int(enum.ActivityTypeDiscordUserUnbanned))).
			WithDefault(b.activityTypeFilter == enum.ActivityTypeDiscordUserUnbanned),
		discord.NewStringSelectMenuOption("Bot Setting Updated", strconv.Itoa(int(enum.ActivityTypeBotSettingUpdated))).
			WithDefault(b.activityTypeFilter == enum.ActivityTypeBotSettingUpdated),
		discord.NewStringSelectMenuOption("Guild Bans", strconv.Itoa(int(enum.ActivityTypeGuildBans))).
			WithDefault(b.activityTypeFilter == enum.ActivityTypeGuildBans),
	}

	// Add options based on current category or activity type
	switch b.categoryFilter {
	case constants.LogsGroupActivityCategoryOption:
		options = append(options, groupOptions...)
		options = append(options,
			discord.NewStringSelectMenuOption("> Show User Activities", constants.LogsUserActivityCategoryOption).
				WithDescription("Show user-related activity types"),
			discord.NewStringSelectMenuOption("> Show Other Activities", constants.LogsOtherActivityCategoryOption).
				WithDescription("Show other activity types"),
		)

	case constants.LogsOtherActivityCategoryOption:
		options = append(options, otherOptions...)
		options = append(options,
			discord.NewStringSelectMenuOption("> Show User Activities", constants.LogsUserActivityCategoryOption).
				WithDescription("Show user-related activity types"),
			discord.NewStringSelectMenuOption("> Show Group Activities", constants.LogsGroupActivityCategoryOption).
				WithDescription("Show group-related activity types"),
		)

	default:
		options = append(options, userOptions...)
		options = append(options,
			discord.NewStringSelectMenuOption("> Show Group Activities", constants.LogsGroupActivityCategoryOption).
				WithDescription("Show group-related activity types"),
			discord.NewStringSelectMenuOption("> Show Other Activities", constants.LogsOtherActivityCategoryOption).
				WithDescription("Show other activity types"),
		)
	}

	return options
}

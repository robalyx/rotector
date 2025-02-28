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
	"github.com/robalyx/rotector/internal/common/storage/database/types"
	"github.com/robalyx/rotector/internal/common/storage/database/types/enum"
)

// Builder creates the visual layout for viewing activity logs.
type Builder struct {
	logs               []*types.ActivityLog
	guildID            uint64
	discordID          uint64
	userID             uint64
	groupID            uint64
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
	// Create embed
	embed := discord.NewEmbedBuilder().
		SetTitle("Log Query Results").
		SetColor(utils.GetMessageEmbedColor(b.privacyMode))

	// Add fields for each active query condition
	if b.guildID != 0 {
		embed.AddField("Guild ID", fmt.Sprintf("`%s`", utils.CensorString(strconv.FormatUint(b.guildID, 10), b.privacyMode)), true)
	}
	if b.discordID != 0 {
		embed.AddField("Discord ID", fmt.Sprintf("`%s`", utils.CensorString(strconv.FormatUint(b.discordID, 10), b.privacyMode)), true)
	}
	if b.userID != 0 {
		embed.AddField("User ID", fmt.Sprintf("`%s`", utils.CensorString(strconv.FormatUint(b.userID, 10), b.privacyMode)), true)
	}
	if b.groupID != 0 {
		embed.AddField("Group ID", fmt.Sprintf("`%s`", utils.CensorString(strconv.FormatUint(b.groupID, 10), b.privacyMode)), true)
	}
	if b.reviewerID != 0 {
		embed.AddField("Reviewer ID", fmt.Sprintf("`%d`", b.reviewerID), true)
	}
	if b.activityTypeFilter != enum.ActivityTypeAll {
		embed.AddField("Activity Type", fmt.Sprintf("`%s`", b.activityTypeFilter), true)
	}
	if !b.startDate.IsZero() && !b.endDate.IsZero() {
		embed.AddField("Date Range", fmt.Sprintf("`%s` to `%s`", b.startDate.Format("2006-01-02"), b.endDate.Format("2006-01-02")), true)
	}

	// Add log entries with details
	if len(b.logs) > 0 {
		for _, log := range b.logs {
			details := ""
			for key, value := range log.Details {
				newKey := strings.ToUpper(key[:1]) + key[1:]
				newValue := utils.NormalizeString(fmt.Sprintf("%v", value))
				details += fmt.Sprintf("\n%s: `%v`", newKey, newValue)
			}

			description := fmt.Sprintf("Activity: `%s`", log.ActivityType.String())

			if log.ActivityTarget.GuildID != 0 {
				description += fmt.Sprintf("\nGuild: %d", log.ActivityTarget.GuildID)
			}

			if log.ActivityTarget.DiscordID != 0 {
				description += fmt.Sprintf("\nDiscord: <@%d>", log.ActivityTarget.DiscordID)
			}

			if log.ActivityTarget.UserID != 0 {
				description += fmt.Sprintf("\nUser: [%s](https://www.roblox.com/users/%d/profile)",
					utils.CensorString(strconv.FormatUint(log.ActivityTarget.UserID, 10), b.privacyMode),
					log.ActivityTarget.UserID)
			}

			if log.ActivityTarget.GroupID != 0 {
				description += fmt.Sprintf("\nGroup: [%s](https://www.roblox.com/groups/%d)",
					utils.CensorString(strconv.FormatUint(log.ActivityTarget.GroupID, 10), b.privacyMode),
					log.ActivityTarget.GroupID)
			}

			description += fmt.Sprintf("\nReviewer: <@%d>%s", log.ReviewerID, details)

			embed.AddField(
				fmt.Sprintf("<t:%d:F>", log.ActivityTimestamp.Unix()),
				description,
				false,
			)
		}
		embed.SetFooterText(fmt.Sprintf("Sequence %d | %d logs shown", b.logs[0].Sequence, len(b.logs)))
	} else {
		embed.AddField("No Results", "No log entries found for the given query", false)
	}

	// Create components
	components := b.buildComponents()

	return discord.NewMessageUpdateBuilder().
		SetEmbeds(embed.Build()).
		AddContainerComponents(components...)
}

// buildComponents creates all interactive components for the log viewer.
func (b *Builder) buildComponents() []discord.ContainerComponent {
	return []discord.ContainerComponent{
		// Query condition selection menu
		discord.NewActionRow(
			discord.NewStringSelectMenu(constants.ActionSelectMenuCustomID, "Set Filter Condition",
				discord.NewStringSelectMenuOption("Filter by Guild ID", constants.LogsQueryGuildIDOption).
					WithDescription(utils.CensorString(strconv.FormatUint(b.guildID, 10), b.privacyMode)),
				discord.NewStringSelectMenuOption("Filter by Discord ID", constants.LogsQueryDiscordIDOption).
					WithDescription(utils.CensorString(strconv.FormatUint(b.discordID, 10), b.privacyMode)),
				discord.NewStringSelectMenuOption("Filter by User ID", constants.LogsQueryUserIDOption).
					WithDescription(utils.CensorString(strconv.FormatUint(b.userID, 10), b.privacyMode)),
				discord.NewStringSelectMenuOption("Filter by Group ID", constants.LogsQueryGroupIDOption).
					WithDescription(utils.CensorString(strconv.FormatUint(b.groupID, 10), b.privacyMode)),
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
			discord.NewSecondaryButton("◀️", constants.BackButtonCustomID),
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
		discord.NewStringSelectMenuOption("User Confirmed", strconv.Itoa(int(enum.ActivityTypeUserConfirmed))).
			WithDefault(b.activityTypeFilter == enum.ActivityTypeUserConfirmed),
		discord.NewStringSelectMenuOption("User Cleared", strconv.Itoa(int(enum.ActivityTypeUserCleared))).
			WithDefault(b.activityTypeFilter == enum.ActivityTypeUserCleared),
		discord.NewStringSelectMenuOption("User Skipped", strconv.Itoa(int(enum.ActivityTypeUserSkipped))).
			WithDefault(b.activityTypeFilter == enum.ActivityTypeUserSkipped),
		discord.NewStringSelectMenuOption("User Rechecked", strconv.Itoa(int(enum.ActivityTypeUserRechecked))).
			WithDefault(b.activityTypeFilter == enum.ActivityTypeUserRechecked),
		discord.NewStringSelectMenuOption("User Training Upvote", strconv.Itoa(int(enum.ActivityTypeUserTrainingUpvote))).
			WithDefault(b.activityTypeFilter == enum.ActivityTypeUserTrainingUpvote),
		discord.NewStringSelectMenuOption("User Training Downvote", strconv.Itoa(int(enum.ActivityTypeUserTrainingDownvote))).
			WithDefault(b.activityTypeFilter == enum.ActivityTypeUserTrainingDownvote),
		discord.NewStringSelectMenuOption("User Deleted", strconv.Itoa(int(enum.ActivityTypeUserDeleted))).
			WithDefault(b.activityTypeFilter == enum.ActivityTypeUserDeleted),
	}

	groupOptions := []discord.StringSelectMenuOption{
		discord.NewStringSelectMenuOption("Group Viewed", strconv.Itoa(int(enum.ActivityTypeGroupViewed))).
			WithDefault(b.activityTypeFilter == enum.ActivityTypeGroupViewed),
		discord.NewStringSelectMenuOption("Group Confirmed", strconv.Itoa(int(enum.ActivityTypeGroupConfirmed))).
			WithDefault(b.activityTypeFilter == enum.ActivityTypeGroupConfirmed),
		discord.NewStringSelectMenuOption("Group Confirmed (Custom)", strconv.Itoa(int(enum.ActivityTypeGroupConfirmedCustom))).
			WithDefault(b.activityTypeFilter == enum.ActivityTypeGroupConfirmedCustom),
		discord.NewStringSelectMenuOption("Group Cleared", strconv.Itoa(int(enum.ActivityTypeGroupCleared))).
			WithDefault(b.activityTypeFilter == enum.ActivityTypeGroupCleared),
		discord.NewStringSelectMenuOption("Group Skipped", strconv.Itoa(int(enum.ActivityTypeGroupSkipped))).
			WithDefault(b.activityTypeFilter == enum.ActivityTypeGroupSkipped),
		discord.NewStringSelectMenuOption("Group Training Upvote", strconv.Itoa(int(enum.ActivityTypeGroupTrainingUpvote))).
			WithDefault(b.activityTypeFilter == enum.ActivityTypeGroupTrainingUpvote),
		discord.NewStringSelectMenuOption("Group Training Downvote", strconv.Itoa(int(enum.ActivityTypeGroupTrainingDownvote))).
			WithDefault(b.activityTypeFilter == enum.ActivityTypeGroupTrainingDownvote),
		discord.NewStringSelectMenuOption("Group Deleted", strconv.Itoa(int(enum.ActivityTypeGroupDeleted))).
			WithDefault(b.activityTypeFilter == enum.ActivityTypeGroupDeleted),
	}

	otherOptions := []discord.StringSelectMenuOption{
		discord.NewStringSelectMenuOption("Appeal Submitted", strconv.Itoa(int(enum.ActivityTypeAppealSubmitted))).
			WithDefault(b.activityTypeFilter == enum.ActivityTypeAppealSubmitted),
		discord.NewStringSelectMenuOption("Appeal Skipped", strconv.Itoa(int(enum.ActivityTypeAppealSkipped))).
			WithDefault(b.activityTypeFilter == enum.ActivityTypeAppealSkipped),
		discord.NewStringSelectMenuOption("Appeal Accepted", strconv.Itoa(int(enum.ActivityTypeAppealAccepted))).
			WithDefault(b.activityTypeFilter == enum.ActivityTypeAppealAccepted),
		discord.NewStringSelectMenuOption("Appeal Rejected", strconv.Itoa(int(enum.ActivityTypeAppealRejected))).
			WithDefault(b.activityTypeFilter == enum.ActivityTypeAppealRejected),
		discord.NewStringSelectMenuOption("Appeal Closed", strconv.Itoa(int(enum.ActivityTypeAppealClosed))).
			WithDefault(b.activityTypeFilter == enum.ActivityTypeAppealClosed),
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
